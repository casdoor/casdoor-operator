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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
)

func newCasdoor(driver string) *operatorv1alpha1.Casdoor {
	return &operatorv1alpha1.Casdoor{
		ObjectMeta: metav1.ObjectMeta{Name: "door", Namespace: "ns"},
		Spec: operatorv1alpha1.CasdoorSpec{
			Database: operatorv1alpha1.DatabaseSpec{Driver: driver},
		},
	}
}

func secretRef(name string, key string) *corev1.SecretKeySelector {
	return &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}
}

func envMap(env []corev1.EnvVar) map[string]corev1.EnvVar {
	res := map[string]corev1.EnvVar{}
	for _, item := range env {
		Expect(res).NotTo(HaveKey(item.Name), "duplicate env %s", item.Name)
		res[item.Name] = item
	}
	return res
}

func envValue(env []corev1.EnvVar, name string) string {
	item, ok := envMap(env)[name]
	Expect(ok).To(BeTrue(), "missing env %s", name)
	return item.Value
}

var _ = Describe("dataSourceName", func() {
	DescribeTable("builds the connection string of each driver",
		func(db operatorv1alpha1.DatabaseSpec, expected string) {
			Expect(dataSourceName(&db)).To(Equal(expected))
		},
		Entry("mysql with a password",
			operatorv1alpha1.DatabaseSpec{Driver: "mysql", Host: "mysql", User: "root", PasswordSecretRef: secretRef("db", "password")},
			"root:$(CASDOOR_DB_PASSWORD)@tcp(mysql:3306)/"),
		Entry("mysql without a password on a custom port",
			operatorv1alpha1.DatabaseSpec{Driver: "mysql", Host: "mysql", Port: 3307, User: "root"},
			"root@tcp(mysql:3307)/"),
		Entry("postgres with defaults",
			operatorv1alpha1.DatabaseSpec{Driver: "postgres", Host: "pg", User: "casdoor", PasswordSecretRef: secretRef("db", "password")},
			"user=casdoor host=pg port=5432 sslmode=disable dbname=casdoor password=$(CASDOOR_DB_PASSWORD)"),
		Entry("postgres with a database name and sslmode",
			operatorv1alpha1.DatabaseSpec{Driver: "postgres", Host: "pg", Port: 6432, User: "u", Name: "iam", SSLMode: "require"},
			"user=u host=pg port=6432 sslmode=require dbname=iam"),
		Entry("sqlite",
			operatorv1alpha1.DatabaseSpec{Driver: "sqlite"},
			"file:/data/casdoor.db?cache=shared&_pragma=busy_timeout(5000)"),
	)
})

var _ = Describe("buildEnv", func() {
	It("runs in prod mode and logs to the console by default", func() {
		env := buildEnv(newCasdoor("sqlite"))
		Expect(envValue(env, "runmode")).To(Equal("prod"))
		Expect(envValue(env, "BEEGO_RUNMODE")).To(Equal("prod"))
		Expect(envValue(env, "logConfig")).To(Equal(`{"adapter":"console"}`))
		Expect(envValue(env, "driverName")).To(Equal("sqlite"))
		Expect(envValue(env, "dbName")).To(Equal("casdoor"))
		Expect(envMap(env)).NotTo(HaveKey("origin"))
	})

	It("ignores config items that dedicated fields manage", func() {
		cr := newCasdoor("sqlite")
		cr.Spec.Config = map[string]string{"dbName": "other", "defaultLanguage": "zh", "runmode": "dev"}
		env := buildEnv(cr)
		Expect(envValue(env, "dbName")).To(Equal("casdoor"))
		Expect(envValue(env, "defaultLanguage")).To(Equal("zh"))
		Expect(envValue(env, "runmode")).To(Equal("dev"))
		Expect(envValue(env, "BEEGO_RUNMODE")).To(Equal("dev"))
	})

	It("lets spec.env replace generated variables", func() {
		cr := newCasdoor("sqlite")
		cr.Spec.Env = []corev1.EnvVar{{Name: "logConfig", Value: "custom"}, {Name: "EXTRA", Value: "1"}}
		env := buildEnv(cr)
		Expect(envValue(env, "logConfig")).To(Equal("custom"))
		Expect(envValue(env, "EXTRA")).To(Equal("1"))
	})

	It("reads secrets through secretKeyRef", func() {
		cr := newCasdoor("postgres")
		cr.Spec.Database.Host = "pg"
		cr.Spec.Database.PasswordSecretRef = secretRef("db", "password")
		cr.Spec.InitAdminPasswordSecretRef = secretRef("admin", "password")
		env := envMap(buildEnv(cr))
		Expect(env["CASDOOR_DB_PASSWORD"].ValueFrom.SecretKeyRef.Name).To(Equal("db"))
		Expect(env["initAdminPassword"].ValueFrom.SecretKeyRef.Name).To(Equal("admin"))
	})

	It("takes the whole dataSourceName from a secret", func() {
		cr := newCasdoor("mysql")
		cr.Spec.Database.DataSourceNameSecretRef = secretRef("dsn", "value")
		env := envMap(buildEnv(cr))
		Expect(env["dataSourceName"].ValueFrom.SecretKeyRef.Key).To(Equal("value"))
		Expect(env).NotTo(HaveKey("CASDOOR_DB_PASSWORD"))
	})

	DescribeTable("builds the redis endpoint",
		func(redis operatorv1alpha1.RedisSpec, expected string) {
			cr := newCasdoor("mysql")
			cr.Spec.Redis = &redis
			Expect(envValue(buildEnv(cr), "redisEndpoint")).To(Equal(expected))
		},
		Entry("plain", operatorv1alpha1.RedisSpec{Endpoint: "redis:6379"}, "redis:6379"),
		Entry("with a password", operatorv1alpha1.RedisSpec{Endpoint: "redis:6379", PasswordSecretRef: secretRef("r", "p")}, "redis:6379,100,$(CASDOOR_REDIS_PASSWORD)"),
		Entry("with a db", operatorv1alpha1.RedisSpec{Endpoint: "redis:6379", DB: 2}, "redis:6379,100,,2"),
	)

	DescribeTable("derives the origin",
		func(origin string, ingress *operatorv1alpha1.IngressSpec, expected string) {
			cr := newCasdoor("sqlite")
			cr.Spec.Origin = origin
			cr.Spec.Ingress = ingress
			Expect(envValue(buildEnv(cr), "origin")).To(Equal(expected))
		},
		Entry("from spec.origin", "https://id.example.org", &operatorv1alpha1.IngressSpec{Host: "door.example.com"}, "https://id.example.org"),
		Entry("from an ingress with TLS", "", &operatorv1alpha1.IngressSpec{Host: "door.example.com", TLSSecretName: "tls"}, "https://door.example.com"),
		Entry("from an ingress without TLS", "", &operatorv1alpha1.IngressSpec{Host: "door.example.com"}, "http://door.example.com"),
	)

	It("points initDataFile to the mounted secret", func() {
		cr := newCasdoor("sqlite")
		cr.Spec.InitData = &operatorv1alpha1.InitDataSpec{SecretRef: *secretRef("init", "data.json"), NewOnly: ptr.To(false)}
		env := buildEnv(cr)
		Expect(envValue(env, "initDataFile")).To(Equal("/init-data/init_data.json"))
		Expect(envValue(env, "initDataNewOnly")).To(Equal("false"))
	})
})

var _ = Describe("buildDeployment", func() {
	It("runs sqlite with Recreate, no createDatabase flag and an emptyDir", func() {
		deployment := buildDeployment(newCasdoor("sqlite"), testImage, "hash")
		Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		container := deployment.Spec.Template.Spec.Containers[0]
		Expect(container.Image).To(Equal(testImage))
		Expect(container.Args).To(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Volumes[0].EmptyDir).NotTo(BeNil())
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(secretHashAnnotation, "hash"))
	})

	It("mounts the data volume claim when storage is set", func() {
		cr := newCasdoor("sqlite")
		cr.Spec.Database.Storage = &operatorv1alpha1.StorageSpec{Size: resource.MustParse("5Gi")}
		deployment := buildDeployment(cr, testImage, "")
		Expect(deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName).To(Equal("door-data"))
		Expect(buildDataPVC(cr).Spec.Resources.Requests.Storage().String()).To(Equal("5Gi"))
	})

	It("creates the database for mysql and postgres unless told not to", func() {
		cr := newCasdoor("postgres")
		cr.Spec.Image = "casbin/casdoor:4.0.0"
		deployment := buildDeployment(cr, testImage, "")
		Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("casbin/casdoor:4.0.0"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"--createDatabase=true"}))

		cr.Spec.Database.Create = ptr.To(false)
		Expect(buildDeployment(cr, testImage, "").Spec.Template.Spec.Containers[0].Args).To(BeEmpty())
		Expect(buildDataPVC(cr)).To(BeNil())
	})

	It("keeps the selector labels when pod labels collide", func() {
		cr := newCasdoor("sqlite")
		cr.Spec.PodLabels = map[string]string{"app.kubernetes.io/name": "other", "team": "iam"}
		labels := buildDeployment(cr, testImage, "").Spec.Template.Labels
		Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/name", "casdoor"))
		Expect(labels).To(HaveKeyWithValue("team", "iam"))
	})
})

var _ = Describe("optional objects", func() {
	It("adds a disruption budget from two replicas", func() {
		cr := newCasdoor("mysql")
		Expect(buildPodDisruptionBudget(cr)).To(BeNil())
		cr.Spec.Replicas = ptr.To(int32(1))
		Expect(buildPodDisruptionBudget(cr)).To(BeNil())
		cr.Spec.Replicas = ptr.To(int32(2))
		Expect(buildPodDisruptionBudget(cr).Spec.MaxUnavailable.IntValue()).To(Equal(1))
	})

	It("adds TLS to the ingress when a secret is given", func() {
		cr := newCasdoor("sqlite")
		Expect(buildIngress(cr)).To(BeNil())
		cr.Spec.Ingress = &operatorv1alpha1.IngressSpec{Host: "door.example.com"}
		Expect(buildIngress(cr).Spec.TLS).To(BeEmpty())
		cr.Spec.Ingress.TLSSecretName = "tls"
		Expect(buildIngress(cr).Spec.TLS[0].Hosts).To(Equal([]string{"door.example.com"}))
	})

	It("uses the service address when there is no origin", func() {
		Expect(instanceURL(newCasdoor("sqlite"))).To(Equal("http://door.ns.svc:8000"))
	})
})
