package free5gcslice

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	bansv1alpha1 "github.com/stevenchiu30801/free5gc-operator/pkg/apis/bans/v1alpha1"
	helm "github.com/stevenchiu30801/onos-bandwidth-operator/pkg/helm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var reqLogger = logf.Log.WithName("controller_free5gcslice")

const finalizerName string = "free5gcslice.finalizer.bans.io"

// State of Free5GCSlice
const (
	StateNull     string = ""
	StateCreating string = "Creating"
	StateRunning  string = "Running"
)

// TODO(dev): Use database or volume to maintain the following soft state
var sliceIdx int = 1
var free5gcsliceMap map[string]int = make(map[string]int)
var controlIpPoolNetworkID24 string = "192.168.2."
var controlIpPoolHostID int = 100
var dataIpPoolNetworkID24 string = "192.168.3."
var dataIpPoolHostID int = 100

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Free5GCSlice Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFree5GCSlice{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("free5gcslice-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Free5GCSlice
	err = c.Watch(&source.Kind{Type: &bansv1alpha1.Free5GCSlice{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Free5GCSlice
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &bansv1alpha1.Free5GCSlice{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileFree5GCSlice implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileFree5GCSlice{}

// ReconcileFree5GCSlice reconciles a Free5GCSlice object
type ReconcileFree5GCSlice struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Free5GCSlice object and makes changes based on the state read
// and what is in the Free5GCSlice.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileFree5GCSlice) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger.Info("Reconciling Free5GCSlice", "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the Free5GCSlice instance
	instance := &bansv1alpha1.Free5GCSlice{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Check if Free5GCSlice object is under deletion
	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted
		// Add the finalizer if not registering it
		if !containsString(instance.ObjectMeta.Finalizers, finalizerName) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, finalizerName)
			if err := r.client.Update(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(instance.ObjectMeta.Finalizers, finalizerName) {
			// The finalizer if present
			// Delete external Helm resources
			smfReleaseName := "free5gc-smf-slice" + strconv.Itoa(free5gcsliceMap[instance.Name])
			if err := helm.UninstallHelmChart(instance.Namespace, smfReleaseName); err != nil {
				return reconcile.Result{}, err
			}

			upfReleaseName := "free5gc-upf-slice" + strconv.Itoa(free5gcsliceMap[instance.Name])
			if err := helm.UninstallHelmChart(instance.Namespace, upfReleaseName); err != nil {
				return reconcile.Result{}, err
			}

			// Wait for SMF and UPF being removed
			for {
				smfName := "free5gc-smf-slice" + strconv.Itoa(free5gcsliceMap[instance.Name])
				smf := &appsv1.Deployment{}
				err := r.client.Get(context.TODO(), types.NamespacedName{Name: smfName, Namespace: instance.Namespace}, smf)
				if err != nil && errors.IsNotFound(err) {
					break
				} else if err != nil {
					return reconcile.Result{}, err
				}
				time.Sleep(1 * time.Second)
			}
			for {
				upfName := "free5gc-upf-slice" + strconv.Itoa(free5gcsliceMap[instance.Name])
				upf := &appsv1.Deployment{}
				err := r.client.Get(context.TODO(), types.NamespacedName{Name: upfName, Namespace: instance.Namespace}, upf)
				if err != nil && errors.IsNotFound(err) {
					break
				} else if err != nil {
					return reconcile.Result{}, err
				}
				time.Sleep(1 * time.Second)
			}

			// Remove finalizer from Free5GCSlice object
			instance.ObjectMeta.Finalizers = removeString(instance.ObjectMeta.Finalizers, finalizerName)
			if err := r.client.Update(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}

		// Stop reconciliation as the object is being deleted
		return reconcile.Result{}, nil
	}

	// Check Free5GCSlice.Status.State, if state is Running or Creating then return and don't requeue
	if instance.Status.State == StateRunning || instance.Status.State == StateCreating {
		return reconcile.Result{}, nil
	} else if instance.Status.State != StateNull {
		err := fmt.Errorf("Unknown Free5GCSlice.Status.State %s", instance.Status.State)
		return reconcile.Result{}, err
	}

	// Update Free5GCSlice.Status.State to Creating
	instance.Status.State = StateCreating
	err = r.client.Status().Update(context.TODO(), instance)
	if err != nil {
		reqLogger.Error(err, "Failed to update Free5GCSlice status")
		return reconcile.Result{}, err
	}

	// Check if Mongo DB already exists, if not create a new one
	mongo := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: "mongo", Namespace: instance.Namespace}, mongo)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Mongo DB", "Namespace", instance.Namespace, "Name", "mongo")

		err = helm.InstallHelmChart(instance.Namespace, "mongo", "mongo", nil)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	} else {
		// Mongo DB already exists
		reqLogger.Info("Mongo DB already exists", "Namespace", mongo.Namespace, "Name", mongo.Name)
	}

	// Create free5GC Helm values
	vals := map[string]interface{}{
		"global": map[string]interface{}{
			"image": map[string]interface{}{
				"free5gc": map[string]interface{}{
					"repository": "docker.winlab.nctu.me/free5gc-build",
					"tag":        "latest",
				},
			},
		},
	}

	// Check if AMF, in representation of free5GC common NFs, already exists, if not create new free5GC cluster
	free5gc := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: "free5gc-amf", Namespace: instance.Namespace}, free5gc)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating free5GC common NFs", "Namespace", instance.Namespace, "Name", "free5gc-common-nf")

		err = helm.InstallHelmChart(instance.Namespace, "free5gc-common-nf", "free5gc", vals)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	} else {
		// free5GC common NFs already exists
		reqLogger.Info("free5GC common NFs already exists", "Namespace", free5gc.Namespace, "Name", free5gc.Name)
	}

	// Create free5GC slice Helm values
	vals["sliceIdx"] = sliceIdx
	vals["supportedSnssaiList"] = instance.Spec.SnssaiList

	// Create a new slice UPF
	reqLogger.Info("Creating free5GC new slice UPF", "Namespace", instance.Namespace, "Name", "free5gc-upf", "S-NSSAIList", instance.Spec.SnssaiList)

	// Create UPF Helm values
	upfPfcpAddr := newControlIP()
	upfGtpuAddr := newDataIP()
	upfVals := vals
	upfVals["pfcp"] = map[string]interface{}{
		"addr": upfPfcpAddr,
	}
	upfVals["gtpu"] = map[string]interface{}{
		"addr": upfGtpuAddr,
	}

	err = helm.InstallHelmChart(instance.Namespace, "free5gc-upf", "free5gc-upf-slice"+strconv.Itoa(sliceIdx), upfVals)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create a new slice SMF
	reqLogger.Info("Creating free5GC new slice SMF", "Namespace", instance.Namespace, "Name", "free5gc-smf", "S-NSSAIList", instance.Spec.SnssaiList)

	// Create SMF Helm values
	smfAddr := newControlIP()
	smfVals := vals
	smfVals["http"] = map[string]interface{}{
		"addr": smfAddr,
	}
	smfVals["pfcp"] = map[string]interface{}{
		"addr": smfAddr,
	}
	smfVals["upf"] = map[string]interface{}{
		"pfcp": map[string]interface{}{
			"addr": upfPfcpAddr,
		},
		"gtpu": map[string]interface{}{
			"addr": upfGtpuAddr,
		},
	}
	smfVals["gnb"] = map[string]interface{}{
		"addr": instance.Spec.GNBAddr,
	}

	err = helm.InstallHelmChart(instance.Namespace, "free5gc-smf", "free5gc-smf-slice"+strconv.Itoa(sliceIdx), smfVals)
	if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Successfully create free5GC network slice", "SliceID", sliceIdx, "S-NSSAIList", instance.Spec.SnssaiList)

	// Maintain mapping between Free5GCSlice object name and slice ID
	free5gcsliceMap[instance.Name] = sliceIdx
	sliceIdx++

	// Update Free5GCSlice.Status.AmfAddr, Free5GCSlice.Status.UpfAddr and Free5GCSlice.Status.State
	amfList := &corev1.PodList{}
	opts := []client.ListOption{
		client.InNamespace(instance.Namespace),
		client.MatchingLabels(map[string]string{"app.kubernetes.io/instance": "free5gc", "app.kubernetes.io/name": "amf"}),
	}
	err = r.client.List(context.TODO(), amfList, opts...)
	if err != nil {
		return reconcile.Result{}, err
	}
	// Access the first AMF
	// Decode IP address of banscore bridge interface from pod metadata
	amfNetworkStatus := amfList.Items[0].ObjectMeta.Annotations["k8s.v1.cni.cncf.io/networks-status"]
	var decoded []map[string]interface{}
	err = json.Unmarshal([]byte(amfNetworkStatus), &decoded)
	if err != nil {
		return reconcile.Result{}, err
	}
	for _, item := range decoded {
		if item["name"].(string) == "banscore" {
			instance.Status.AmfAddr = item["ips"].([]interface{})[0].(string)
			break
		}
	}
	instance.Status.UpfAddr = upfGtpuAddr
	instance.Status.State = StateRunning
	err = r.client.Status().Update(context.TODO(), instance)
	if err != nil {
		reqLogger.Error(err, "Failed to update Free5GCSlice status")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// Helper functions to check and remove string from a slice of strings.
// See https://github.com/kubernetes-sigs/kubebuilder/blob/master/docs/book/src/cronjob-tutorial/testdata/finalizer_example.go

// containsString checks if the given slice of string contains the target string
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// removeString removes the target string from the given slice of string
func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

// newControlIP returns an available control plane IP in string
// TODO(dev): Maintain control plane IP pool to support release of IPs
func newControlIP() string {
	newIp := controlIpPoolNetworkID24 + strconv.Itoa(controlIpPoolHostID)
	controlIpPoolHostID++

	return newIp
}

// newDataIP returns an available data plane IP in string
// TODO(dev): Maintain data plane IP pool to support release of IPs
func newDataIP() string {
	newIp := dataIpPoolNetworkID24 + strconv.Itoa(dataIpPoolHostID)
	dataIpPoolHostID++

	return newIp
}
