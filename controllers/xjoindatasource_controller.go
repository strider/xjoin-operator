package controllers

import (
	"context"
	"github.com/go-errors/errors"
	"github.com/go-logr/logr"
	xjoin "github.com/redhatinsights/xjoin-operator/api/v1alpha1"
	"github.com/redhatinsights/xjoin-operator/controllers/common"
	"github.com/redhatinsights/xjoin-operator/controllers/config"
	. "github.com/redhatinsights/xjoin-operator/controllers/datasource"
	xjoinlogger "github.com/redhatinsights/xjoin-operator/controllers/log"
	"github.com/redhatinsights/xjoin-operator/controllers/parameters"
	k8sUtils "github.com/redhatinsights/xjoin-operator/controllers/utils"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

type XJoinDataSourceReconciler struct {
	Client    client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Namespace string
	Test      bool
}

func NewXJoinDataSourceReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	log logr.Logger,
	recorder record.EventRecorder,
	namespace string,
	isTest bool) *XJoinDataSourceReconciler {

	return &XJoinDataSourceReconciler{
		Client:    client,
		Log:       log,
		Scheme:    scheme,
		Recorder:  recorder,
		Namespace: namespace,
		Test:      isTest,
	}
}

func (r *XJoinDataSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logConstructor := func(r *reconcile.Request) logr.Logger {
		return mgr.GetLogger()
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("xjoin-datasource-controller").
		For(&xjoin.XJoinDataSource{}).
		WithLogConstructor(logConstructor).
		WithOptions(controller.Options{
			LogConstructor: logConstructor,
			RateLimiter:    workqueue.NewItemExponentialFailureRateLimiter(time.Millisecond, 1*time.Minute),
		}).
		Complete(r)
}

// +kubebuilder:rbac:groups=xjoin.cloud.redhat.com,resources=xjoindatasources;xjoindatasources/status;xjoindatasources/finalizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps;pods,verbs=get;list;watch

func (r *XJoinDataSourceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := xjoinlogger.NewLogger("controller_xjoindatasource", "DataSource", request.Name, "Namespace", request.Namespace)
	reqLogger.Info("Reconciling XJoinDataSource")

	instance, err := k8sUtils.FetchXJoinDataSource(r.Client, request.NamespacedName, ctx)
	if err != nil {
		if k8errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return result, nil
		}
		// Error reading the object - requeue the request.
		return result, errors.Wrap(err, 0)
	}

	p := parameters.BuildDataSourceParameters()

	if p.Pause.Bool() {
		return
	}

	configManager, err := config.NewManager(config.ManagerOptions{
		Client:         r.Client,
		Parameters:     p,
		ConfigMapNames: []string{"xjoin-generic"},
		SecretNames:    nil,
		Namespace:      instance.Namespace,
		Spec:           instance.Spec,
		Context:        ctx,
		Log:            reqLogger,
	})
	if err != nil {
		return result, errors.Wrap(err, 0)
	}

	err = configManager.Parse()
	if err != nil {
		return result, errors.Wrap(err, 0)
	}

	originalInstance := instance.DeepCopy()
	i := XJoinDataSourceIteration{
		Parameters: *p,
		Iteration: common.Iteration{
			Context:          ctx,
			Instance:         instance,
			OriginalInstance: originalInstance,
			Client:           r.Client,
			Log:              reqLogger,
			Test:             r.Test,
		},
	}

	if err = i.AddFinalizer(i.GetFinalizerName()); err != nil {
		return reconcile.Result{}, errors.Wrap(err, 0)
	}

	dataSourceReconciler := NewReconcileMethods(i, common.DataSourceGVK)
	reconciler := common.NewReconciler(dataSourceReconciler, instance, reqLogger)
	err = reconciler.Reconcile(false)
	if err != nil {
		return result, errors.Wrap(err, 0)
	}

	if instance.GetDeletionTimestamp() != nil {
		//actual finalizer code is called via reconciler
		return reconcile.Result{}, nil
	}

	instance.Status.SpecHash, err = k8sUtils.SpecHash(instance.Spec)
	if err != nil {
		return result, errors.Wrap(err, 0)
	}

	//TODO actually validate
	if originalInstance.Status.RefreshingVersion != "" {
		instance.Status.ActiveVersionIsValid = true
		instance.Status.ActiveVersion = instance.Status.RefreshingVersion
	}

	return i.UpdateStatusAndRequeue(time.Second * 30)
}
