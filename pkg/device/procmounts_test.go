// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseMountInfoLine", func() {
	DescribeTable("should parse valid lines",
		func(line string, want MountEntry) {
			got, err := parseMountInfoLine(line)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.MountID).To(Equal(want.MountID))
			Expect(got.ParentID).To(Equal(want.ParentID))
			Expect(got.Major).To(Equal(want.Major))
			Expect(got.Minor).To(Equal(want.Minor))
			Expect(got.MountPoint).To(Equal(want.MountPoint))
			Expect(got.FSType).To(Equal(want.FSType))
			Expect(got.Source).To(Equal(want.Source))
		},
		Entry("ext4 mount",
			"36 35 8:1 / /mnt/data rw,noatime shared:1 - ext4 /dev/sda1 rw,errors=continue",
			MountEntry{MountID: 36, ParentID: 35, Major: 8, Minor: 1, Root: "/", MountPoint: "/mnt/data", FSType: "ext4", Source: "/dev/sda1"},
		),
		Entry("NFS mount",
			"100 50 0:45 / /var/lib/kubelet/pods/abc/volumes/nfs-vol/mount rw,relatime shared:10 - nfs 10.0.0.1:/export rw",
			MountEntry{MountID: 100, ParentID: 50, Major: 0, Minor: 45, Root: "/", MountPoint: "/var/lib/kubelet/pods/abc/volumes/nfs-vol/mount", FSType: "nfs", Source: "10.0.0.1:/export"},
		),
		Entry("device mapper",
			"42 35 253:3 / /var/lib/containers rw,relatime shared:2 - xfs /dev/mapper/rhel-containers rw,seclabel",
			MountEntry{MountID: 42, ParentID: 35, Major: 253, Minor: 3, Root: "/", MountPoint: "/var/lib/containers", FSType: "xfs", Source: "/dev/mapper/rhel-containers"},
		),
	)

	DescribeTable("should return error for invalid lines",
		func(line string) {
			_, err := parseMountInfoLine(line)
			Expect(err).To(HaveOccurred())
		},
		Entry("too few fields", "36 35 8:1"),
		Entry("missing separator", "36 35 8:1 / /mnt rw ext4 /dev/sda1 rw"),
	)
})

var _ = Describe("ParseMountInfo", func() {
	It("should parse all entries from a mountinfo file", func() {
		content := `22 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw,errors=continue
36 22 253:0 / /home rw,noatime shared:2 - xfs /dev/mapper/vg0-home rw
100 22 0:45 / /mnt/nfs rw - nfs 10.0.0.1:/share rw
`
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "mountinfo")
		Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())

		entries, err := ParseMountInfo(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(3))
		Expect(entries[0].FSType).To(Equal("ext4"))
		Expect(entries[1].FSType).To(Equal("xfs"))
		Expect(entries[2].FSType).To(Equal("nfs"))
	})
})
