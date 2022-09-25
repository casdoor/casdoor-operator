/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"casdoor-operator/controllers/utils"
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strconv"
	"strings"
	"time"

	operatorv1 "casdoor-operator/api/v1"
)

const (
	FinalizerName = "casdoor.org/finalizer"
)

// CasdoorReconciler reconciles a Casdoor object
type CasdoorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoors/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Casdoor object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *CasdoorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start to reconcile Casdoor")

	casdoor := &operatorv1.Casdoor{}
	if err := r.Get(ctx, req.NamespacedName, casdoor); err != nil {
		logger.Error(err, "unable to fetch Casdoor")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !casdoor.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.delete(ctx, casdoor)
	} else {
		err := r.apply(ctx, casdoor)
		if err != nil {
			casdoor.Status.Status = operatorv1.CasdoorStatusFailed
			casdoor.Status.Reason = err.Error()
			_ = r.Status().Update(ctx, casdoor)
		}
		return ctrl.Result{}, err
	}
}

func (r *CasdoorReconciler) apply(ctx context.Context, casdoor *operatorv1.Casdoor) error {
	// Add finalizer
	if controllerutil.AddFinalizer(casdoor, FinalizerName) {
		if err := r.Status().Update(ctx, casdoor); err != nil {
			return err
		}
	}

	if err := r.applyConfigMap(ctx, casdoor); err != nil {
		return err
	}
	if err := r.applyService(ctx, casdoor); err != nil {
		return err
	}
	if err := r.applyDeployment(ctx, casdoor); err != nil {
		return err
	}
	return nil
}

func (r *CasdoorReconciler) applyConfigMap(ctx context.Context, casdoor *operatorv1.Casdoor) error {
	objectMeta := metav1.ObjectMeta{
		Name:      casdoor.Name,
		Namespace: casdoor.Namespace,
	}
	appConf := utils.MergeAppConf(casdoor.Spec.AppConf)
	expectConfigMap := &corev1.ConfigMap{
		ObjectMeta: objectMeta,
		Data: map[string]string{
			"app.conf":       appConf,
			"init_data.json": casdoor.Spec.InitData,
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: objectMeta,
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Data = expectConfigMap.Data
		if err := controllerutil.SetControllerReference(casdoor, configMap, r.Scheme); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *CasdoorReconciler) applyDeployment(ctx context.Context, casdoor *operatorv1.Casdoor) error {
	objectMeta := metav1.ObjectMeta{
		Name:      casdoor.Name,
		Namespace: casdoor.Namespace,
	}
	// Different image need different commands and args
	var commands, args []string
	if !strings.Contains(casdoor.Spec.Image, "all-in-one") {
		logger := log.FromContext(ctx)
		logger.Info("Casdoor image is not all-in-one, will use default commands and args")
		commands = append(commands, "/bin/sh")
		args = append(args, "-c", "./server --createDatabase=true")
	}
	httpPort, err := casdoor.GetHttpPort()
	if err != nil {
		return err
	}

	expectDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      casdoor.Name,
			Namespace: casdoor.Namespace,
			// Restart Pod
			Labels: map[string]string{"updateTimestamp": strconv.FormatInt(time.Now().Unix(), 10)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: casdoor.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": casdoor.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": casdoor.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "casdoor",
							Image:           casdoor.Spec.Image,
							ImagePullPolicy: corev1.PullPolicy(casdoor.Spec.ImagePullPolicy),
							Command:         commands,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: httpPort,
								},
							},
							Env: []corev1.EnvVar{
								{Name: "RUN_IN_DOCKER", Value: "true"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      casdoor.Name,
									MountPath: "/init_data.json",
									SubPath:   "init_data.json",
								},
								{
									Name:      casdoor.Name,
									MountPath: "/conf/app.conf",
									SubPath:   "app.conf",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: casdoor.Name,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: casdoor.Name,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: objectMeta,
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// Deployment selector is immutable, so we set this value only if
		// a new object is going to be created
		if deployment.ObjectMeta.CreationTimestamp.IsZero() {
			deployment.Spec.Selector = expectDeployment.Spec.Selector
		}
		deployment.Spec.Replicas = expectDeployment.Spec.Replicas
		deployment.Spec.Template.ObjectMeta = expectDeployment.Spec.Template.ObjectMeta
		deployment.Spec.Template.Spec.Volumes = expectDeployment.Spec.Template.Spec.Volumes

		// not override existed auto-generated items
		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			deployment.Spec.Template.Spec.Containers = expectDeployment.Spec.Template.Spec.Containers
		} else {
			deployment.Spec.Template.Spec.Containers[0].Name = expectDeployment.Spec.Template.Spec.Containers[0].Name
			deployment.Spec.Template.Spec.Containers[0].Image = expectDeployment.Spec.Template.Spec.Containers[0].Image
			deployment.Spec.Template.Spec.Containers[0].Ports = expectDeployment.Spec.Template.Spec.Containers[0].Ports
			deployment.Spec.Template.Spec.Containers[0].Env = expectDeployment.Spec.Template.Spec.Containers[0].Env
		}

		if err := controllerutil.SetControllerReference(casdoor, deployment, r.Scheme); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// Deployment is real payload to run Casdoor,
	// so only update status to running if deployment is successfully created
	if deployment.Status.AvailableReplicas == *casdoor.Spec.Replicas {
		casdoor.Status.Status = operatorv1.CasdoorStatusRunning
		return r.Status().Update(ctx, casdoor)
	}
	return nil
}

func (r *CasdoorReconciler) applyService(ctx context.Context, casdoor *operatorv1.Casdoor) error {
	objectMeta := metav1.ObjectMeta{
		Name:      casdoor.Name,
		Namespace: casdoor.Namespace,
		Labels:    map[string]string{"app": casdoor.Name},
	}
	httpPort, err := casdoor.GetHttpPort()
	if err != nil {
		return err
	}

	expectService := &corev1.Service{
		ObjectMeta: objectMeta,
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http", Port: httpPort, Protocol: corev1.ProtocolTCP},
			},
			Selector: map[string]string{"app": casdoor.Name},
		},
	}
	service := &corev1.Service{
		ObjectMeta: objectMeta,
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec = expectService.Spec
		if err := controllerutil.SetControllerReference(casdoor, service, r.Scheme); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *CasdoorReconciler) delete(ctx context.Context, casdoor *operatorv1.Casdoor) error {
	if err := r.Delete(ctx, casdoor); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CasdoorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("casdoor-controller")
	owner := &handler.EnqueueRequestForOwner{OwnerType: &operatorv1.Casdoor{}, IsController: false}
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1.Casdoor{}).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, owner).
		Complete(r)
}
