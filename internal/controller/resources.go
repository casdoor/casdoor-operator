// Copyright 2026 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
)

const (
	containerName = "casdoor"
	httpPortName  = "http"
	httpPort      = 8000

	dataVolumeName     = "data"
	dataMountPath      = "/data"
	initDataVolumeName = "init-data"
	initDataMountPath  = "/init-data"
	initDataFileName   = "init_data.json"

	secretHashAnnotation = "operator.casdoor.org/secret-hash"

	dbPasswordEnv    = "CASDOOR_DB_PASSWORD"
	redisPasswordEnv = "CASDOOR_REDIS_PASSWORD"
	redisPoolSize    = 100
	casdoorUserId    = 1000
)

// These app.conf items are generated from dedicated spec fields.
var managedConfigKeys = map[string]bool{
	"httpport":          true,
	"driverName":        true,
	"dataSourceName":    true,
	"dbName":            true,
	"origin":            true,
	"redisEndpoint":     true,
	"initAdminPassword": true,
	"initDataFile":      true,
	"initDataNewOnly":   true,
}

func selectorLabels(cr *operatorv1alpha1.Casdoor) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "casdoor",
		"app.kubernetes.io/instance": cr.Name,
	}
}

func objectLabels(cr *operatorv1alpha1.Casdoor) map[string]string {
	labels := selectorLabels(cr)
	labels["app.kubernetes.io/managed-by"] = "casdoor-operator"
	return labels
}

func objectMeta(cr *operatorv1alpha1.Casdoor, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: cr.Namespace,
		Labels:    objectLabels(cr),
	}
}

func dataPVCName(cr *operatorv1alpha1.Casdoor) string {
	return cr.Name + "-data"
}

func servicePort(cr *operatorv1alpha1.Casdoor) int32 {
	if cr.Spec.Service.Port != 0 {
		return cr.Spec.Service.Port
	}
	return httpPort
}

func origin(cr *operatorv1alpha1.Casdoor) string {
	if cr.Spec.Origin != "" {
		return cr.Spec.Origin
	}
	if cr.Spec.Ingress != nil {
		if cr.Spec.Ingress.TLSSecretName != "" {
			return "https://" + cr.Spec.Ingress.Host
		}
		return "http://" + cr.Spec.Ingress.Host
	}
	return ""
}

func instanceURL(cr *operatorv1alpha1.Casdoor) string {
	if url := origin(cr); url != "" {
		return url
	}
	return fmt.Sprintf("http://%s.%s.svc:%d", cr.Name, cr.Namespace, servicePort(cr))
}

func dataSourceName(db *operatorv1alpha1.DatabaseSpec) string {
	password := ""
	if db.PasswordSecretRef != nil {
		password = "$(" + dbPasswordEnv + ")"
	}

	switch db.Driver {
	case operatorv1alpha1.DriverMySQL:
		port := db.Port
		if port == 0 {
			port = 3306
		}
		credential := db.User
		if password != "" {
			credential += ":" + password
		}
		return fmt.Sprintf("%s@tcp(%s:%d)/", credential, db.Host, port)
	case operatorv1alpha1.DriverPostgres:
		port := db.Port
		if port == 0 {
			port = 5432
		}
		sslMode := db.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		dsn := fmt.Sprintf("user=%s host=%s port=%d sslmode=%s dbname=%s", db.User, db.Host, port, sslMode, databaseName(db))
		if password != "" {
			dsn += " password=" + password
		}
		return dsn
	default:
		return fmt.Sprintf("file:%s/casdoor.db?cache=shared&_pragma=busy_timeout(5000)", dataMountPath)
	}
}

func databaseName(db *operatorv1alpha1.DatabaseSpec) string {
	if db.Name != "" {
		return db.Name
	}
	return "casdoor"
}

func secretEnv(name string, ref *corev1.SecretKeySelector) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: ref.LocalObjectReference, Key: ref.Key},
		},
	}
}

func buildEnv(cr *operatorv1alpha1.Casdoor) []corev1.EnvVar {
	config := map[string]string{
		"runmode":   "prod",
		"logConfig": `{"adapter":"console"}`,
	}
	for key, value := range cr.Spec.Config {
		if !managedConfigKeys[key] {
			config[key] = value
		}
	}

	env := []corev1.EnvVar{
		{Name: "BEEGO_RUNMODE", Value: config["runmode"]},
	}

	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, corev1.EnvVar{Name: key, Value: config[key]})
	}

	db := &cr.Spec.Database
	env = append(env,
		corev1.EnvVar{Name: "driverName", Value: db.Driver},
		corev1.EnvVar{Name: "dbName", Value: databaseName(db)},
	)
	if db.DataSourceNameSecretRef != nil {
		env = append(env, secretEnv("dataSourceName", db.DataSourceNameSecretRef))
	} else {
		if db.PasswordSecretRef != nil {
			env = append(env, secretEnv(dbPasswordEnv, db.PasswordSecretRef))
		}
		env = append(env, corev1.EnvVar{Name: "dataSourceName", Value: dataSourceName(db)})
	}

	if url := origin(cr); url != "" {
		env = append(env, corev1.EnvVar{Name: "origin", Value: url})
	}

	if redis := cr.Spec.Redis; redis != nil {
		endpoint := redis.Endpoint
		if redis.PasswordSecretRef != nil || redis.DB != 0 {
			endpoint += fmt.Sprintf(",%d,", redisPoolSize)
			if redis.PasswordSecretRef != nil {
				env = append(env, secretEnv(redisPasswordEnv, redis.PasswordSecretRef))
				endpoint += "$(" + redisPasswordEnv + ")"
			}
			if redis.DB != 0 {
				endpoint += fmt.Sprintf(",%d", redis.DB)
			}
		}
		env = append(env, corev1.EnvVar{Name: "redisEndpoint", Value: endpoint})
	}

	if cr.Spec.InitAdminPasswordSecretRef != nil {
		env = append(env, secretEnv("initAdminPassword", cr.Spec.InitAdminPasswordSecretRef))
	}

	if initData := cr.Spec.InitData; initData != nil {
		newOnly := initData.NewOnly == nil || *initData.NewOnly
		env = append(env,
			corev1.EnvVar{Name: "initDataFile", Value: initDataMountPath + "/" + initDataFileName},
			corev1.EnvVar{Name: "initDataNewOnly", Value: strconv.FormatBool(newOnly)},
		)
	}

	return mergeEnv(env, cr.Spec.Env)
}

// mergeEnv lets user variables replace generated ones of the same name; names must stay unique for server-side apply.
func mergeEnv(generated []corev1.EnvVar, extra []corev1.EnvVar) []corev1.EnvVar {
	overridden := map[string]bool{}
	for _, item := range extra {
		overridden[item.Name] = true
	}
	res := make([]corev1.EnvVar, 0, len(generated)+len(extra))
	for _, item := range generated {
		if !overridden[item.Name] {
			res = append(res, item)
		}
	}
	seen := map[string]int{}
	for _, item := range extra {
		if index, ok := seen[item.Name]; ok {
			res[index] = item
			continue
		}
		seen[item.Name] = len(res)
		res = append(res, item)
	}
	return res
}

func buildDeployment(cr *operatorv1alpha1.Casdoor, defaultImage string, secretHash string) *appsv1.Deployment {
	image := cr.Spec.Image
	if image == "" {
		image = defaultImage
	}

	podLabels := map[string]string{}
	for key, value := range cr.Spec.PodLabels {
		podLabels[key] = value
	}
	for key, value := range selectorLabels(cr) {
		podLabels[key] = value
	}

	podAnnotations := map[string]string{}
	for key, value := range cr.Spec.PodAnnotations {
		podAnnotations[key] = value
	}
	podAnnotations[secretHashAnnotation] = secretHash

	db := &cr.Spec.Database
	var args []string
	if db.Driver != operatorv1alpha1.DriverSQLite && (db.Create == nil || *db.Create) {
		args = []string{"--createDatabase=true"}
	}

	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount
	strategy := appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType}
	if db.Driver == operatorv1alpha1.DriverSQLite {
		// The sqlite file cannot be opened by the old and the new pod at the same time.
		strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
		source := corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
		if db.Storage != nil {
			source = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: dataPVCName(cr)},
			}
		}
		volumes = append(volumes, corev1.Volume{Name: dataVolumeName, VolumeSource: source})
		mounts = append(mounts, corev1.VolumeMount{Name: dataVolumeName, MountPath: dataMountPath})
	}
	if initData := cr.Spec.InitData; initData != nil {
		volumes = append(volumes, corev1.Volume{
			Name: initDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: initData.SecretRef.Name,
					Items:      []corev1.KeyToPath{{Key: initData.SecretRef.Key, Path: initDataFileName}},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: initDataVolumeName, MountPath: initDataMountPath, ReadOnly: true})
	}

	health := corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{Path: "/api/health", Port: intstr.FromString(httpPortName)},
	}
	userId := int64(casdoorUserId)
	nonRoot := true

	return &appsv1.Deployment{
		ObjectMeta: objectMeta(cr, cr.Name),
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.Replicas,
			Strategy: strategy,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(cr)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels, Annotations: podAnnotations},
				Spec: corev1.PodSpec{
					ServiceAccountName:        cr.Spec.ServiceAccountName,
					ImagePullSecrets:          cr.Spec.ImagePullSecrets,
					PriorityClassName:         cr.Spec.PriorityClassName,
					NodeSelector:              cr.Spec.NodeSelector,
					Tolerations:               cr.Spec.Tolerations,
					Affinity:                  cr.Spec.Affinity,
					TopologySpreadConstraints: cr.Spec.TopologySpreadConstraints,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &nonRoot,
						RunAsUser:    &userId,
						RunAsGroup:   &userId,
						FSGroup:      &userId,
					},
					Containers: []corev1.Container{
						{
							Name:            containerName,
							Image:           image,
							ImagePullPolicy: cr.Spec.ImagePullPolicy,
							Args:            args,
							Env:             buildEnv(cr),
							Ports: []corev1.ContainerPort{
								{Name: httpPortName, ContainerPort: httpPort, Protocol: corev1.ProtocolTCP},
							},
							Resources:    cr.Spec.Resources,
							VolumeMounts: mounts,
							StartupProbe: &corev1.Probe{
								ProbeHandler:     health,
								PeriodSeconds:    5,
								FailureThreshold: 60,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler:  health,
								PeriodSeconds: 10,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler:     health,
								PeriodSeconds:    20,
								FailureThreshold: 3,
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}
}

func buildService(cr *operatorv1alpha1.Casdoor) *corev1.Service {
	meta := objectMeta(cr, cr.Name)
	meta.Annotations = cr.Spec.Service.Annotations

	serviceType := cr.Spec.Service.Type
	if serviceType == "" {
		serviceType = corev1.ServiceTypeClusterIP
	}

	return &corev1.Service{
		ObjectMeta: meta,
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: selectorLabels(cr),
			Ports: []corev1.ServicePort{
				{
					Name:       httpPortName,
					Port:       servicePort(cr),
					TargetPort: intstr.FromString(httpPortName),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func buildIngress(cr *operatorv1alpha1.Casdoor) *networkingv1.Ingress {
	spec := cr.Spec.Ingress
	if spec == nil {
		return nil
	}

	meta := objectMeta(cr, cr.Name)
	meta.Annotations = spec.Annotations
	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: meta,
		Spec: networkingv1.IngressSpec{
			IngressClassName: spec.ClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: spec.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: cr.Name,
											Port: networkingv1.ServiceBackendPort{Name: httpPortName},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if spec.TLSSecretName != "" {
		ingress.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{spec.Host}, SecretName: spec.TLSSecretName}}
	}
	return ingress
}

func buildPodDisruptionBudget(cr *operatorv1alpha1.Casdoor) *policyv1.PodDisruptionBudget {
	if cr.Spec.Replicas == nil || *cr.Spec.Replicas < 2 {
		return nil
	}

	maxUnavailable := intstr.FromInt32(1)
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: objectMeta(cr, cr.Name),
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       &metav1.LabelSelector{MatchLabels: selectorLabels(cr)},
		},
	}
}

func buildDataPVC(cr *operatorv1alpha1.Casdoor) *corev1.PersistentVolumeClaim {
	db := &cr.Spec.Database
	if db.Driver != operatorv1alpha1.DriverSQLite || db.Storage == nil {
		return nil
	}

	size := db.Storage.Size
	if size.IsZero() {
		size = resource.MustParse("1Gi")
	}
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: objectMeta(cr, dataPVCName(cr)),
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: db.Storage.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
		},
	}
}
