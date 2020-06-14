package webhookrelayforward

import (
	"context"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	forwardv1 "github.com/webhookrelay/webhookrelay-operator/pkg/apis/forward/v1"
	"github.com/webhookrelay/webhookrelay-operator/pkg/config"
)

var log = logf.Log.WithName("controller_webhookrelayforward")

const (
	reconcilePeriodSeconds = 15

	// containerTokenKeyEnvName and containerTokenSecretEnvName used
	// to specify authentication details for the container
	containerTokenKeyEnvName    = "KEY"
	containerTokenSecretEnvName = "SECRET"
	// containerBucketsEnvName specify which buckets the agent should
	// subscribe to
	containerBucketsEnvName = "BUCKETS"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new WebhookRelayForward Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	cfg := config.MustLoad()
	return &ReconcileWebhookRelayForward{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		config: &cfg,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("webhookrelayforward-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource WebhookRelayForward
	err = c.Watch(&source.Kind{Type: &forwardv1.WebhookRelayForward{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Deployments and requeue the owner WebhookRelayForward
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &forwardv1.WebhookRelayForward{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileWebhookRelayForward implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileWebhookRelayForward{}

// ReconcileWebhookRelayForward reconciles a WebhookRelayForward object
type ReconcileWebhookRelayForward struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme

	apiClient *WebhookRelayClient
	config    *config.Config
}

// Reconcile reads that state of the cluster for a WebhookRelayForward object and makes changes based on the state read
// and what is in the WebhookRelayForward.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileWebhookRelayForward) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	reconcilePeriod := reconcilePeriodSeconds * time.Second
	reconcileResult := reconcile.Result{RequeueAfter: reconcilePeriod}

	// Fetch the WebhookRelayForward instance
	instance := &forwardv1.WebhookRelayForward{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcileResult, err
	}

	// Compare the instance names, generations and UIDs to check if it's
	// the same instance. Update the client if client instance name,
	// generation or UID are different from current instance. In theory,
	// CRs can be used by different Webhook Relay accounts so we shouldn't
	// reuse the same client
	if r.apiClient == nil ||
		r.apiClient.instanceName != instance.GetName() ||
		r.apiClient.instanceGeneration != instance.GetGeneration() ||
		r.apiClient.instanceUID != instance.GetUID() {
		if err := r.setClientForCluster(instance); err != nil {
			log.Error(err, "Failed to configure Webhook Relay API client, cannot continue")
			return reconcileResult, err
		}
	}

	if err := r.reconcile(logger, instance); err != nil {
		logger.Info("Reconcile failed", "error", err)
		return reconcileResult, nil
	}

	return reconcileResult, nil
}

func (r *ReconcileWebhookRelayForward) reconcile(logger logr.Logger, instance *forwardv1.WebhookRelayForward) error {

	// Define a new Deployment object
	deployment := r.newDeploymentForCR(instance)

	// Set WebhookRelayForward instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, deployment, r.scheme); err != nil {
		return err
	}

	// Check if this Deployment already exists
	found := &appsv1.Deployment{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.client.Create(context.TODO(), deployment)
		if err != nil {
			return err
		}

		// Deployment created successfully - don't requeue
		return nil
	} else if err != nil {
		return err
	}

	// compare image, buckets

	// Deployment already exists - don't requeue
	logger.Info("Skip reconcile: Deployment already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)

	return nil
}

// newDeploymentForCR returns a new Webhook Relay forwarder deployment with the same name/namespace as the cr
func (r *ReconcileWebhookRelayForward) newDeploymentForCR(cr *forwardv1.WebhookRelayForward) *appsv1.Deployment {
	labels := map[string]string{
		"app": cr.Name,
	}
	podLabels := map[string]string{
		"name": "webhookrelay-forwarder",
	}

	var buckets []string
	for idx := range cr.Spec.Buckets {
		buckets = append(buckets, cr.Spec.Buckets[idx].Ref)
	}

	image := cr.Spec.Image
	if image == "" {
		image = r.config.Image
	}

	// TODO: refactor this part
	// as a mounted secret
	// for access token https://github.com/webhookrelay/webhookrelay-operator/issues/1
	env := []corev1.EnvVar{
		{
			Name:  containerBucketsEnvName,
			Value: strings.Join(buckets, ","),
		},
	}

	// configuring authentication for the container
	if cr.Spec.SecretRefName != "" {

		keyRefSelect := &corev1.SecretKeySelector{}
		keyRefSelect.Name = cr.Spec.SecretRefName
		keyRefSelect.Key = forwardv1.AccessTokenKeyName

		secretRefSelect := &corev1.SecretKeySelector{}
		secretRefSelect.Name = cr.Spec.SecretRefName
		secretRefSelect.Key = forwardv1.AccessTokenSecretName

		env = append(env,
			corev1.EnvVar{
				Name: containerTokenKeyEnvName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: keyRefSelect,
				},
			},
			corev1.EnvVar{
				Name: containerTokenSecretEnvName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: secretRefSelect,
				},
			},
		)
	} else {
		// setting the ones from the client that have likely come
		// from the environment variables set directly on the operator
		env = append(env,
			corev1.EnvVar{
				Name:  containerTokenKeyEnvName,
				Value: r.apiClient.accessTokenKey,
			},
			corev1.EnvVar{
				Name:  containerTokenSecretEnvName,
				Value: r.apiClient.accessTokenSecret,
			},
		)
	}

	podTemplateSpec := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "webhookrelayd",
					ImagePullPolicy: corev1.PullAlways,
					Image:           image,
					Env:             env,
				},
			},
		},
	}
	podTemplateSpec.Labels = podLabels
	podTemplateSpec.Name = "webhookrelay"
	// TODO: set namespace
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-whr-deployment",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: toInt32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "webhookrelay-forwarder",
				},
			},
			Template: podTemplateSpec,
		},
	}
}

func toInt32(val int32) *int32 {
	return &val
}
