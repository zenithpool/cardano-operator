package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cardanov1 "github.com/zenithpool/cardano-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Core Controllers", func() {
	var (
		key types.NamespacedName
	)

	const timeout = time.Second * 5
	const interval = time.Second * 1

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
		key = types.NamespacedName{
			Name:      "ginkgo-core",
			Namespace: "default",
		}
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
		f := &cardanov1.Core{}
		_ = k8sClient.Get(context.Background(), key, f)
		k8sClient.Delete(context.Background(), f)
	})

	Context("Default Core", func() {
		It("should create core node", func() {

			hostPath := "hostpath"

			new := &cardanov1.Core{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: cardanov1.CoreSpec{
					NodeSpec: cardanov1.NodeSpec{
						Replicas:         1,
						ImagePullSecrets: []v1.LocalObjectReference{{Name: "ocirsecret"}},
						Image:            "fra.ocir.io/axj3k4dkrqku/cardano-node:1.19.0",
						Storage: v1.PersistentVolumeClaimSpec{
							AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
							StorageClassName: &hostPath,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceName(v1.ResourceStorage): resource.MustParse("1Gi")},
							},
						},
						Service: cardanov1.NodeServiceSpec{
							Type: v1.ServiceTypeClusterIP,
							Port: 31400,
						},
					},
				},
			}

			By("Create Core instance")
			Expect(k8sClient.Create(context.Background(), new)).Should(Succeed())

			By("Expecting submitted")
			Eventually(func() *cardanov1.Core {
				f := &cardanov1.Core{}
				_ = k8sClient.Get(context.Background(), key, f)
				return f
			}).ShouldNot(BeNil())

			Eventually(func() string {
				f := &appsv1.StatefulSet{}
				_ = k8sClient.Get(context.Background(), key, f)
				return f.Spec.ServiceName
			}, timeout, interval).Should(Equal(key.Name))

			Eventually(func() int32 {
				f := &appsv1.StatefulSet{}
				_ = k8sClient.Get(context.Background(), key, f)
				return *f.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(1)))

			By("Update core to 2 replicas")
			new.Spec.Replicas = 2
			Expect(k8sClient.Update(context.Background(), new)).Should(Succeed())

			Eventually(func() int32 {
				f := &appsv1.StatefulSet{}
				_ = k8sClient.Get(context.Background(), key, f)
				return *f.Spec.Replicas
			}, timeout, interval).Should(Equal(int32(2)))

			By("Update core to image")
			new.Spec.Image = "fra.ocir.io/axj3k4dkrqku/cardano-node:1.18.0"
			Expect(k8sClient.Update(context.Background(), new)).Should(Succeed())

			Eventually(func() string {
				f := &appsv1.StatefulSet{}
				_ = k8sClient.Get(context.Background(), key, f)
				if len(f.Spec.Template.Spec.Containers) == 1 {
					return f.Spec.Template.Spec.Containers[0].Image
				}
				return ""
			}, timeout, interval).Should(Equal("fra.ocir.io/axj3k4dkrqku/cardano-node:1.18.0"))

			By("Expecting to delete successfully")
			Eventually(func() error {
				f := &cardanov1.Core{}
				_ = k8sClient.Get(context.Background(), key, f)
				return k8sClient.Delete(context.Background(), f)
			}, timeout, interval).Should(Succeed())

			By("Expecting to delete finish")
			Eventually(func() error {
				f := &cardanov1.Core{}
				return k8sClient.Get(context.Background(), key, f)
			}, timeout, interval).ShouldNot(Succeed())

		})
	})

})
