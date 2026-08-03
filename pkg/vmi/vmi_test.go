package vmi

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var _ = Describe("ParseDomainName", func() {
	It("splits on first underscore", func() {
		ns, name, ok := ParseDomainName("mynamespace_myvmi")
		Expect(ok).To(BeTrue())
		Expect(ns).To(Equal("mynamespace"))
		Expect(name).To(Equal("myvmi"))
	})

	It("handles VMI names that contain underscores", func() {
		ns, name, ok := ParseDomainName("ns_vmi_with_underscores")
		Expect(ok).To(BeTrue())
		Expect(ns).To(Equal("ns"))
		Expect(name).To(Equal("vmi_with_underscores"))
	})

	It("returns false when there is no underscore", func() {
		_, _, ok := ParseDomainName("nodomain")
		Expect(ok).To(BeFalse())
	})

	It("returns false when underscore is first character", func() {
		_, _, ok := ParseDomainName("_vmi")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("ReadDomainName", func() {
	var procRoot string

	BeforeEach(func() {
		procRoot = GinkgoT().TempDir()
	})

	writeCmdline := func(pid int, args ...string) {
		pidDir := filepath.Join(procRoot, fmt.Sprintf("%d", pid))
		Expect(os.MkdirAll(pidDir, 0755)).To(Succeed())
		var data []byte
		for i, arg := range args {
			if i > 0 {
				data = append(data, 0)
			}
			data = append(data, []byte(arg)...)
		}
		Expect(os.WriteFile(filepath.Join(pidDir, "cmdline"), data, 0644)).To(Succeed())
	}

	It("extracts guest= prefixed name", func() {
		writeCmdline(1000, "/usr/bin/qemu-system-x86_64", "-name", "guest=testns_myvm,debug-threads=on", "-m", "2048")
		name, ok := ReadDomainName(1000, procRoot)
		Expect(ok).To(BeTrue())
		Expect(name).To(Equal("testns_myvm"))
	})

	It("extracts plain name without guest= prefix", func() {
		writeCmdline(1000, "/usr/bin/qemu-system-x86_64", "-name", "testns_myvm")
		name, ok := ReadDomainName(1000, procRoot)
		Expect(ok).To(BeTrue())
		Expect(name).To(Equal("testns_myvm"))
	})

	It("returns false when -name flag is absent", func() {
		writeCmdline(1000, "/usr/bin/qemu-system-x86_64", "-m", "2048")
		_, ok := ReadDomainName(1000, procRoot)
		Expect(ok).To(BeFalse())
	})

	It("returns false for missing PID", func() {
		_, ok := ReadDomainName(99999, procRoot)
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("BuildDomainToPodMap", func() {
	virtLauncherPod := func(namespace, vmiName, podName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels: map[string]string{
					"kubevirt.io":         "virt-launcher",
					"vm.kubevirt.io/name": vmiName,
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}

	newStore := func(pods ...*corev1.Pod) cache.Store {
		store := cache.NewStore(cache.MetaNamespaceKeyFunc)
		for _, p := range pods {
			Expect(store.Add(p)).To(Succeed())
		}
		return store
	}

	It("maps running virt-launcher pods", func() {
		store := newStore(
			virtLauncherPod("ns1", "vm1", "virt-launcher-vm1-abc"),
			virtLauncherPod("ns2", "vm2", "virt-launcher-vm2-xyz"),
		)
		m := BuildDomainToPodMap(store)
		Expect(m).To(Equal(map[string]string{
			"ns1_vm1": "virt-launcher-vm1-abc",
			"ns2_vm2": "virt-launcher-vm2-xyz",
		}))
	})

	It("excludes non-running pods", func() {
		pod := virtLauncherPod("ns", "vm", "virt-launcher-vm-abc")
		pod.Status.Phase = corev1.PodPending
		Expect(BuildDomainToPodMap(newStore(pod))).To(BeEmpty())
	})

	It("excludes pods without the virt-launcher label", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-pod",
				Namespace: "ns",
				Labels:    map[string]string{"vm.kubevirt.io/name": "vm"},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
		Expect(BuildDomainToPodMap(newStore(pod))).To(BeEmpty())
	})
})
