package controllers_test

import (
	"context"
	"github.com/jarcoal/httpmock"
	. "github.com/onsi/gomega"
	"github.com/redhatinsights/xjoin-operator/api/v1alpha1"
	"github.com/redhatinsights/xjoin-operator/controllers"
	"github.com/redhatinsights/xjoin-operator/controllers/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type XJoinIndexPipelineTestReconciler struct {
	Namespace            string
	Name                 string
	ConfigFileName       string
	CustomSubgraphImages []v1alpha1.CustomSubgraphImage
	K8sClient            client.Client
	DataSources          []DataSource
	createdIndexPipeline v1alpha1.XJoinIndexPipeline
}

type DataSource struct {
	Name                     string
	Version                  string
	ApiCurioResponseFilename string
}

func (x *XJoinIndexPipelineTestReconciler) ReconcileNew() v1alpha1.XJoinIndexPipeline {
	x.registerNewMocks()
	x.createValidIndexPipeline()
	result := x.reconcile()
	Expect(result).To(Equal(reconcile.Result{Requeue: false, RequeueAfter: 30000000000}))
	indexLookupKey := types.NamespacedName{Name: x.Name, Namespace: x.Namespace}
	Eventually(func() bool {
		err := x.K8sClient.Get(context.Background(), indexLookupKey, &x.createdIndexPipeline)
		return err == nil
	}, K8sGetTimeout, K8sGetInterval).Should(BeTrue())
	return x.createdIndexPipeline
}

func (x *XJoinIndexPipelineTestReconciler) ReconcileDelete() {
	x.registerDeleteMocks()
	result := x.reconcile()
	Expect(result).To(Equal(reconcile.Result{Requeue: false, RequeueAfter: 0}))

	indexPipelineList := v1alpha1.XJoinIndexPipelineList{}
	err := x.K8sClient.List(context.Background(), &indexPipelineList, client.InNamespace(x.Namespace))
	checkError(err)
	Expect(indexPipelineList.Items).To(HaveLen(0))
}

func (x *XJoinIndexPipelineTestReconciler) reconcile() reconcile.Result {
	ctx := context.Background()
	xjoinIndexPipelineReconciler := x.newXJoinIndexPipelineReconciler()
	indexLookupKey := types.NamespacedName{Name: x.Name, Namespace: x.Namespace}
	result, err := xjoinIndexPipelineReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: indexLookupKey})
	checkError(err)
	return result
}

func (x *XJoinIndexPipelineTestReconciler) registerDeleteMocks() {
	httpmock.Reset()
	httpmock.RegisterNoResponder(httpmock.InitialTransport.RoundTrip) //disable mocks for unregistered http requests

	//connector mocks
	httpmock.RegisterResponder(
		"GET",
		"http://connect-connect-api."+x.Namespace+".svc:8083/connectors/xjoinindexpipeline."+x.Name+".1234",
		httpmock.NewStringResponder(404, `{}`))

	//gql schema mocks
	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline-"+x.Name+"-1234/versions/1",
		httpmock.NewStringResponder(200, `{}`))
	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline-"+x.Name+"-1234/versions/latest",
		httpmock.NewStringResponder(200, `{}`))
	httpmock.RegisterResponder(
		"DELETE",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline-"+x.Name+"-1234",
		httpmock.NewStringResponder(200, `{}`))

	//elasticsearch index mocks
	httpmock.RegisterResponder(
		"HEAD",
		"http://localhost:9200/xjoinindexpipeline."+x.Name+".1234",
		httpmock.NewStringResponder(200, `{}`))
	httpmock.RegisterResponder(
		"DELETE",
		"http://localhost:9200/xjoinindexpipeline."+x.Name+".1234",
		httpmock.NewStringResponder(200, `{}`))

	//avro schema mocks
	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline."+x.Name+".1234-value/versions/1",
		httpmock.NewStringResponder(200, `{}`))

	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline."+x.Name+".1234-value/versions/latest",
		httpmock.NewStringResponder(200, `{}`))

	httpmock.RegisterResponder(
		"DELETE",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline."+x.Name+".1234-value",
		httpmock.NewStringResponder(200, `{}`))

	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline."+x.Name+".1234/versions",
		httpmock.NewStringResponder(404, `{}`))

	for _, customImage := range x.CustomSubgraphImages {
		httpmock.RegisterResponder(
			"GET",
			"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline.test-index-pipeline-"+customImage.Name+".1234/versions",
			httpmock.NewStringResponder(200, `{}`).Once())

		httpmock.RegisterResponder(
			"DELETE",
			"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline.test-index-pipeline-"+customImage.Name+".1234",
			httpmock.NewStringResponder(200, `{}`).Once())
	}

	for _, dataSource := range x.DataSources {
		response, err := os.ReadFile("./test/data/apicurio/" + dataSource.ApiCurioResponseFilename + ".json")
		checkError(err)
		httpmock.RegisterResponder(
			"GET",
			"http://apicurio:1080/apis/ccompat/v6/subjects/xjoindatasourcepipeline."+dataSource.Name+"."+dataSource.Version+"-value/versions/latest",
			httpmock.NewStringResponder(200, string(response)))

		httpmock.RegisterResponder(
			"GET",
			"http://localhost:9200/_ingest/pipeline/xjoinindexpipeline.test-index-pipeline.1234",
			httpmock.NewStringResponder(200, "{}").Once())

		httpmock.RegisterResponder(
			"DELETE",
			"http://localhost:9200/_ingest/pipeline/xjoinindexpipeline.test-index-pipeline.1234",
			httpmock.NewStringResponder(200, "{}").Once())
	}
}

func (x *XJoinIndexPipelineTestReconciler) registerNewMocks() {
	httpmock.Reset()
	httpmock.RegisterNoResponder(httpmock.InitialTransport.RoundTrip) //disable mocks for unregistered http requests

	//elasticsearch index mocks
	httpmock.RegisterResponder(
		"HEAD",
		"http://localhost:9200/xjoinindexpipeline."+x.Name+".1234",
		httpmock.NewStringResponder(404, `{}`))

	httpmock.RegisterResponder(
		"PUT",
		"http://localhost:9200/xjoinindexpipeline."+x.Name+".1234",
		httpmock.NewStringResponder(201, `{}`))

	//avro schema mocks
	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline."+x.Name+".1234-value/versions/1",
		httpmock.NewStringResponder(404, `{"message":"No version '1' found for artifact with ID 'xjoinindexpipelinepipeline.`+x.Name+`.1234-value' in group 'null'.","error_code":40402}`))

	httpmock.RegisterResponder(
		"POST",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline."+x.Name+".1234-value/versions",
		httpmock.NewStringResponder(200, `{"createdBy":"","createdOn":"2022-07-27T17:28:11+0000","modifiedBy":"","modifiedOn":"2022-07-27T17:28:11+0000","id":1,"version":1,"type":"AVRO","globalId":1,"state":"ENABLED","groupId":"null","contentId":1,"references":[]}`))

	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/schemas/ids/1",
		httpmock.NewStringResponder(200, `{"schema":"{\"name\":\"Value\",\"namespace\":\"xjoindatasourcepipeline.`+x.Name+`\"}","schemaType":"AVRO","references":[]}`))

	//graphql schema mocks
	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline."+x.Name+".1234/versions",
		httpmock.NewStringResponder(404, `{}`))

	httpmock.RegisterResponder(
		"POST",
		"http://apicurio:1080/apis/registry/v2/groups/default/artifacts",
		httpmock.NewStringResponder(201, `{}`))

	httpmock.RegisterResponder(
		"PUT",
		"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline."+x.Name+".1234/meta",
		httpmock.NewStringResponder(200, `{}`))

	for _, customImage := range x.CustomSubgraphImages {
		//custom subgraph graphql schema mocks
		httpmock.RegisterResponder(
			"GET",
			"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline.test-index-pipeline-"+customImage.Name+".1234/versions",
			httpmock.NewStringResponder(404, `{}`))

		httpmock.RegisterResponder(
			"POST",
			"http://apicurio:1080/apis/registry/v2/groups/default/artifacts",
			httpmock.NewStringResponder(201, `{}`))

		httpmock.RegisterResponder(
			"PUT",
			"http://apicurio:1080/apis/registry/v2/groups/default/artifacts/xjoinindexpipeline.test-index-pipeline-"+customImage.Name+".1234/meta",
			httpmock.NewStringResponder(200, `{}`))
	}

	for _, dataSource := range x.DataSources {
		response, err := os.ReadFile("./test/data/apicurio/" + dataSource.ApiCurioResponseFilename + ".json")
		checkError(err)
		httpmock.RegisterResponder(
			"GET",
			"http://apicurio:1080/apis/ccompat/v6/subjects/xjoindatasourcepipeline."+dataSource.Name+"."+dataSource.Version+"-value/versions/latest",
			httpmock.NewStringResponder(200, string(response)))

		httpmock.RegisterResponder(
			"GET",
			"http://localhost:9200/_ingest/pipeline/xjoinindexpipeline.test-index-pipeline.1234",
			httpmock.NewStringResponder(404, "{}").Once())

		httpmock.RegisterResponder(
			"PUT",
			"http://localhost:9200/_ingest/pipeline/xjoinindexpipeline.test-index-pipeline.1234",
			httpmock.NewStringResponder(200, "{}"))
	}

	httpmock.RegisterResponder(
		"GET",
		"http://apicurio:1080/apis/ccompat/v6/subjects/xjoinindexpipeline."+x.Name+".1234-value/versions/latest",
		httpmock.NewStringResponder(200, "{}"))
}

func (x *XJoinIndexPipelineTestReconciler) newXJoinIndexPipelineReconciler() *controllers.XJoinIndexPipelineReconciler {
	return controllers.NewXJoinIndexPipelineReconciler(
		x.K8sClient,
		scheme.Scheme,
		testLogger,
		record.NewFakeRecorder(10),
		x.Namespace,
		true)
}

func (x *XJoinIndexPipelineTestReconciler) createValidIndexPipeline() {
	ctx := context.Background()
	indexAvroSchema, err := os.ReadFile("./test/data/avro/" + x.ConfigFileName + ".json")
	checkError(err)
	xjoinIndexName := "test-xjoin-index"

	//XjoinIndexPipeline requires an XJoinIndex owner. Create one here
	indexSpec := v1alpha1.XJoinIndexSpec{
		AvroSchema:           string(indexAvroSchema),
		Pause:                false,
		CustomSubgraphImages: x.CustomSubgraphImages,
	}

	index := &v1alpha1.XJoinIndex{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xjoinIndexName,
			Namespace: x.Namespace,
		},
		Spec: indexSpec,
		TypeMeta: metav1.TypeMeta{
			APIVersion: "xjoin.cloud.redhat.com/v1alpha1",
			Kind:       "XJoinIndex",
		},
	}

	Expect(x.K8sClient.Create(ctx, index)).Should(Succeed())

	//create the XJoinIndexPipeline
	indexPipelineSpec := v1alpha1.XJoinIndexPipelineSpec{
		Name:                 x.Name,
		Version:              "1234",
		AvroSchema:           string(indexAvroSchema),
		Pause:                false,
		CustomSubgraphImages: x.CustomSubgraphImages,
	}

	blockOwnerDeletion := true
	controller := true
	indexPipeline := &v1alpha1.XJoinIndexPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      x.Name,
			Namespace: x.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         common.IndexGVK.Version,
					Kind:               common.IndexGVK.Kind,
					Name:               xjoinIndexName,
					Controller:         &controller,
					BlockOwnerDeletion: &blockOwnerDeletion,
					UID:                "a6778b9b-dfed-4d41-af53-5ebbcddb7535",
				},
			},
		},
		Spec: indexPipelineSpec,
		TypeMeta: metav1.TypeMeta{
			APIVersion: "xjoin.cloud.redhat.com/v1alpha1",
			Kind:       "XJoinIndexPipeline",
		},
	}

	Expect(x.K8sClient.Create(ctx, indexPipeline)).Should(Succeed())

	//validate indexPipeline spec is created correctly
	indexPipelineLookupKey := types.NamespacedName{Name: x.Name, Namespace: x.Namespace}
	createdIndexPipeline := &v1alpha1.XJoinIndexPipeline{}

	Eventually(func() bool {
		err := x.K8sClient.Get(ctx, indexPipelineLookupKey, createdIndexPipeline)
		return err == nil
	}, K8sGetTimeout, K8sGetInterval).Should(BeTrue())

	Expect(createdIndexPipeline.Spec.Name).Should(Equal(x.Name))
	Expect(createdIndexPipeline.Spec.Version).Should(Equal("1234"))
	Expect(createdIndexPipeline.Spec.Pause).Should(Equal(false))
	Expect(createdIndexPipeline.Spec.AvroSchema).Should(Equal(string(indexAvroSchema)))
	Expect(createdIndexPipeline.Spec.CustomSubgraphImages).Should(Equal(x.CustomSubgraphImages))
}
