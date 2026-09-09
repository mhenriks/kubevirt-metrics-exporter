// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var _ = Describe("parseKubeletVolumePath", func() {
	DescribeTable("should parse paths",
		func(mountPoint, wantUID, wantPVC string, wantOK bool) {
			uid, pvc, ok := parseKubeletVolumePath(mountPoint)
			Expect(ok).To(Equal(wantOK))
			if wantOK {
				Expect(uid).To(Equal(wantUID))
				Expect(pvc).To(Equal(wantPVC))
			}
		},
		Entry("NFS volume",
			"/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/volumes/kubernetes.io~nfs/example",
			"6a76960a-a927-4211-96e6-1f187b126e90", "example", true),
		Entry("CSI volume with /mount suffix",
			"/var/lib/kubelet/pods/aabbccdd-1234-5678-9abc-def012345678/volumes/kubernetes.io~csi/pvc-deadbeef-0000-1111-2222-333344445555/mount",
			"aabbccdd-1234-5678-9abc-def012345678", "pvc-deadbeef-0000-1111-2222-333344445555", true),
		Entry("local volume",
			"/var/lib/kubelet/pods/11111111-2222-3333-4444-555566667777/volumes/kubernetes.io~local-volume/local-pv-abc",
			"11111111-2222-3333-4444-555566667777", "local-pv-abc", true),
		Entry("non-kubelet mount", "/mnt/data", "", "", false),
		Entry("root filesystem", "/", "", "", false),
		Entry("kubelet path without volume",
			"/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/containers/app",
			"", "", false),
		Entry("invalid UUID in path",
			"/var/lib/kubelet/pods/not-a-uuid/volumes/kubernetes.io~nfs/example",
			"", "", false),
		Entry("block device path does not match filesystem regex",
			"/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/c68a7381-7595-4de4-a594-cffe21c042fe",
			"", "", false),
	)
})

var _ = Describe("parseKubeletBlockDevicePath", func() {
	DescribeTable("should parse paths",
		func(mountPoint, wantUID, wantPVC string, wantOK bool) {
			uid, pvc, ok := parseKubeletBlockDevicePath(mountPoint)
			Expect(ok).To(Equal(wantOK))
			if wantOK {
				Expect(uid).To(Equal(wantUID))
				Expect(pvc).To(Equal(wantPVC))
			}
		},
		Entry("CSI block volume publish path",
			"/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/c68a7381-7595-4de4-a594-cffe21c042fe",
			"c68a7381-7595-4de4-a594-cffe21c042fe", "pvc-30f5aca9-100b-4759-b9d9-5d896081fd23", true),
		Entry("staging path (no pod UID)",
			"/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/staging/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/0001-0009-rook-ceph-0000000000000001-474bbf8f-2bec-4025-8391-5b316cdb801f",
			"", "", false),
		Entry("dev path (no pod UID at end)",
			"/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/dev/c68a7381-7595-4de4-a594-cffe21c042fe",
			"", "", false),
		Entry("filesystem volume path does not match block regex",
			"/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/volumes/kubernetes.io~nfs/example",
			"", "", false),
	)
})

var _ = Describe("Resolver.resolvePVCName", func() {
	var r *Resolver

	BeforeEach(func() {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
			PVCByPVIndexName: PVCByPVIndexFunc,
		})
		indexer.Add(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "my-data-pvc", Namespace: "default"},
			Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "pvc-abcd-1234"},
		})
		r = &Resolver{pvcIndexer: indexer}
	})

	DescribeTable("should resolve PV names to PVC names",
		func(pvName, want string) {
			Expect(r.resolvePVCName(pvName)).To(Equal(want))
		},
		Entry("resolves PV to PVC name", "pvc-abcd-1234", "my-data-pvc"),
		Entry("returns PV name when not found", "pvc-unknown", "pvc-unknown"),
		Entry("returns empty string as-is", "", ""),
	)

	It("should return the PV name when indexer is nil", func() {
		r := &Resolver{}
		Expect(r.resolvePVCName("pvc-abcd-1234")).To(Equal("pvc-abcd-1234"))
	})
})

var _ = Describe("Resolver.podMetaMap", func() {
	It("should return metadata for all pods in the store", func() {
		store := cache.NewStore(cache.MetaNamespaceKeyFunc)
		store.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "virt-launcher-my-vm-abc", Namespace: "production", UID: "uid-1234"},
		})
		store.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "default", UID: "uid-5678"},
		})

		r := &Resolver{podStore: store}
		metas := r.podMetaMap()

		Expect(metas).To(HaveLen(2))
		Expect(metas["uid-1234"].Name).To(Equal("virt-launcher-my-vm-abc"))
		Expect(metas["uid-1234"].Namespace).To(Equal("production"))
		Expect(metas["uid-5678"].Name).To(Equal("other-pod"))
	})

	It("should return empty map when store is nil", func() {
		r := &Resolver{}
		Expect(r.podMetaMap()).To(BeEmpty())
	})
})
