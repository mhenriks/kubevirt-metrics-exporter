package cgroup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func fakePodStore(pods ...*corev1.Pod) cache.Store {
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)
	for _, p := range pods {
		Expect(store.Add(p)).To(Succeed())
	}
	return store
}

func virtLauncherPod(namespace, vmiName, podName string) *corev1.Pod {
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

func collectMetrics(c prometheus.Collector) map[string][]*dto.Metric {
	reg := prometheus.NewRegistry()
	Expect(reg.Register(c)).To(Succeed())
	mfs, err := reg.Gather()
	Expect(err).NotTo(HaveOccurred())

	result := make(map[string][]*dto.Metric)
	for _, mf := range mfs {
		result[mf.GetName()] = mf.GetMetric()
	}
	return result
}

func checkLabels(m *dto.Metric, want map[string]string) {
	got := make(map[string]string, len(m.Label))
	for _, lp := range m.Label {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		Expect(got).To(HaveKeyWithValue(k, v), fmt.Sprintf("label %q", k))
	}
}

var _ = Describe("readMemoryStat", func() {
	var cgroupRoot string

	BeforeEach(func() {
		cgroupRoot = GinkgoT().TempDir()
	})

	It("parses cgroup v2 memory.stat", func() {
		dir := filepath.Join(cgroupRoot, "kubepods.slice", "test-scope")
		Expect(os.MkdirAll(dir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "memory.stat"), []byte(
			"anon 1048576\n"+
				"file 524288\n"+
				"active_anon 262144\n"+
				"inactive_anon 131072\n"+
				"anon_thp 2097152\n"+
				"shmem_thp 1048576\n"+
				"file_thp 524288\n"+
				"pgfault 100\n",
		), 0644)).To(Succeed())

		stat, err := readMemoryStat(cgroupRoot, "/kubepods.slice/test-scope")
		Expect(err).NotTo(HaveOccurred())
		Expect(stat.activeAnon).To(Equal(uint64(262144)))
		Expect(stat.inactiveAnon).To(Equal(uint64(131072)))
		Expect(stat.anonTHP).To(Equal(uint64(2097152)))
		Expect(stat.shmemTHP).To(Equal(uint64(1048576)))
		Expect(stat.fileTHP).To(Equal(uint64(524288)))
	})

	It("returns error for missing directory", func() {
		_, err := readMemoryStat(cgroupRoot, "/nonexistent")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("readCgroupV2Path", func() {
	var procRoot string

	BeforeEach(func() {
		procRoot = GinkgoT().TempDir()
	})

	It("reads the cgroup v2 path", func() {
		pidDir := filepath.Join(procRoot, "1234")
		Expect(os.MkdirAll(pidDir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(
			"0::/kubepods.slice/kubepods-burstable.slice/crio-abc123.scope\n",
		), 0644)).To(Succeed())

		path, err := readCgroupV2Path(1234, procRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(path).To(Equal("/kubepods.slice/kubepods-burstable.slice/crio-abc123.scope"))
	})

	It("returns error for cgroup v1 only entries", func() {
		pidDir := filepath.Join(procRoot, "1234")
		Expect(os.MkdirAll(pidDir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(
			"12:memory:/kubepods/pod123\n11:cpu:/kubepods/pod123\n",
		), 0644)).To(Succeed())

		_, err := readCgroupV2Path(1234, procRoot)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("resolveCgroupPath", func() {
	It("resolves namespace-relative path", func() {
		nsRoot := "/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-podAAA.slice/crio-BBB.scope"
		nsRel := "/../../kubepods-burstable-podXXX.slice/crio-YYY.scope"
		Expect(resolveCgroupPath(nsRel, nsRoot)).To(Equal(
			"/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-podXXX.slice/crio-YYY.scope"))
	})

	It("passes through absolute paths when nsRoot is empty", func() {
		Expect(resolveCgroupPath("/kubepods.slice/test.scope", "")).To(Equal("/kubepods.slice/test.scope"))
	})
})

var _ = Describe("readProcessCPUSeconds", func() {
	var procRoot string

	BeforeEach(func() {
		procRoot = GinkgoT().TempDir()
	})

	It("reads utime + stime and converts to seconds", func() {
		pidDir := filepath.Join(procRoot, "42")
		Expect(os.MkdirAll(pidDir, 0755)).To(Succeed())
		// Fields: pid (comm) state ppid pgrp session tty_nr tpgid flags
		//         minflt cminflt majflt cmajflt utime stime ...
		Expect(os.WriteFile(filepath.Join(pidDir, "stat"), []byte(
			"42 (khugepaged) S 2 0 0 0 -1 2129984 0 0 0 0 500 300 0 0 20 0 1 0 100 0 0",
		), 0644)).To(Succeed())

		cpu, err := readProcessCPUSeconds(42, procRoot)
		Expect(err).NotTo(HaveOccurred())
		// (500 + 300) / 100 = 8.0
		Expect(cpu).To(Equal(8.0))
	})

	It("handles comm with spaces", func() {
		pidDir := filepath.Join(procRoot, "99")
		Expect(os.MkdirAll(pidDir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pidDir, "stat"), []byte(
			"99 (has space) S 2 0 0 0 -1 2129984 0 0 0 0 1000 0 0 0 20 0 1 0 100 0 0",
		), 0644)).To(Succeed())

		cpu, err := readProcessCPUSeconds(99, procRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(cpu).To(Equal(10.0))
	})
})

var _ = Describe("findKernelThreadPID", func() {
	var procRoot string

	BeforeEach(func() {
		procRoot = GinkgoT().TempDir()
	})

	writeComm := func(pid int, comm string) {
		pidDir := filepath.Join(procRoot, fmt.Sprintf("%d", pid))
		Expect(os.MkdirAll(pidDir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pidDir, "comm"), []byte(comm+"\n"), 0644)).To(Succeed())
	}

	It("finds a kernel thread by name", func() {
		writeComm(1, "systemd")
		writeComm(50, "khugepaged")
		writeComm(51, "ksmd")

		pid, err := findKernelThreadPID("khugepaged", procRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(pid).To(Equal(50))
	})

	It("returns error when not found", func() {
		writeComm(1, "systemd")
		_, err := findKernelThreadPID("khugepaged", procRoot)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("readKSMGeneralProfit", func() {
	var sysRoot string

	BeforeEach(func() {
		sysRoot = GinkgoT().TempDir()
	})

	It("reads a positive value", func() {
		ksmDir := filepath.Join(sysRoot, "kernel", "mm", "ksm")
		Expect(os.MkdirAll(ksmDir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(ksmDir, "general_profit"), []byte("1044480\n"), 0644)).To(Succeed())

		profit, ok := readKSMGeneralProfit(sysRoot)
		Expect(ok).To(BeTrue())
		Expect(profit).To(Equal(int64(1044480)))
	})

	It("reads a negative value (overhead exceeds savings)", func() {
		ksmDir := filepath.Join(sysRoot, "kernel", "mm", "ksm")
		Expect(os.MkdirAll(ksmDir, 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(ksmDir, "general_profit"), []byte("-8192\n"), 0644)).To(Succeed())

		profit, ok := readKSMGeneralProfit(sysRoot)
		Expect(ok).To(BeTrue())
		Expect(profit).To(Equal(int64(-8192)))
	})

	It("returns false when file does not exist", func() {
		_, ok := readKSMGeneralProfit(sysRoot)
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Collector end-to-end (synthetic)", func() {
	It("emits all VMI memory metrics with correct labels and values", func() {
		store := fakePodStore(virtLauncherPod("testns", "myvm", "virt-launcher-myvm-abc"))

		c := NewCollector(Config{
			NodeName:   "node1",
			CgroupRoot: "/unused",
			ProcPath:   "/unused",
		}, store, nil, slog.Default())

		c.mu.Lock()
		c.vmiResults = []vmiMemStats{{
			namespace:    "testns",
			name:         "myvm",
			pod:          "virt-launcher-myvm-abc",
			activeAnon:   262144,
			inactiveAnon: 131072,
			anonTHP:      2097152,
			shmemTHP:     1048576,
			fileTHP:      524288,
		}}
		c.lastPollTS = 1000
		c.mu.Unlock()

		metrics := collectMetrics(c)

		wantLabels := map[string]string{
			"namespace": "testns",
			"name":      "myvm",
			"node":      "node1",
			"pod":       "virt-launcher-myvm-abc",
		}

		By("checking active_anon")
		m := metrics["container_memory_active_anon_bytes"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Gauge.GetValue()).To(Equal(float64(262144)))
		checkLabels(m[0], wantLabels)

		By("checking inactive_anon")
		m = metrics["container_memory_inactive_anon_bytes"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Gauge.GetValue()).To(Equal(float64(131072)))

		By("checking anon_thp")
		m = metrics["container_memory_anon_thp_bytes"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Gauge.GetValue()).To(Equal(float64(2097152)))

		By("checking shmem_thp")
		m = metrics["container_memory_shmem_thp_bytes"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Gauge.GetValue()).To(Equal(float64(1048576)))

		By("checking file_thp")
		m = metrics["container_memory_file_thp_bytes"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Gauge.GetValue()).To(Equal(float64(524288)))

		By("checking operational metrics are present")
		Expect(metrics["kme_cgroup_scrape_errors_total"]).NotTo(BeEmpty())
		Expect(metrics["kme_cgroup_last_poll_timestamp_seconds"]).NotTo(BeEmpty())
	})

	It("emits node-level metrics when kernel threads are available", func() {
		store := fakePodStore()

		c := NewCollector(Config{
			NodeName:   "node1",
			CgroupRoot: "/unused",
			ProcPath:   "/unused",
		}, store, nil, slog.Default())

		c.mu.Lock()
		c.node = nodeStats{
			khugepageCPU:       42.5,
			khugepageAvailable: true,
			ksmdCPU:            12.3,
			ksmdAvailable:      true,
			ksmProfit:          1044480,
			ksmProfitAvailable: true,
		}
		c.lastPollTS = 1000
		c.mu.Unlock()

		metrics := collectMetrics(c)

		By("checking khugepaged CPU counter")
		m := metrics["kme_cgroup_khugepaged_cpu_seconds_total"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Counter.GetValue()).To(Equal(42.5))
		checkLabels(m[0], map[string]string{"node": "node1"})

		By("checking ksmd CPU counter")
		m = metrics["kme_cgroup_ksmd_cpu_seconds_total"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Counter.GetValue()).To(Equal(12.3))
		checkLabels(m[0], map[string]string{"node": "node1"})

		By("checking KSM general_profit gauge")
		m = metrics["node_ksmd_general_profit_bytes"]
		Expect(m).To(HaveLen(1))
		Expect(m[0].Gauge.GetValue()).To(Equal(float64(1044480)))
	})

	It("omits node metrics when kernel threads are not available", func() {
		store := fakePodStore()

		c := NewCollector(Config{
			NodeName:   "node1",
			CgroupRoot: "/unused",
			ProcPath:   "/unused",
		}, store, nil, slog.Default())

		c.mu.Lock()
		c.node = nodeStats{
			khugepageAvailable: false,
			ksmdAvailable:      false,
			ksmProfitAvailable: false,
		}
		c.lastPollTS = 1000
		c.mu.Unlock()

		metrics := collectMetrics(c)

		Expect(metrics["kme_cgroup_khugepaged_cpu_seconds_total"]).To(BeEmpty())
		Expect(metrics["kme_cgroup_ksmd_cpu_seconds_total"]).To(BeEmpty())
		Expect(metrics["node_ksmd_general_profit_bytes"]).To(BeEmpty())
	})

	It("emits metrics for multiple VMIs", func() {
		store := fakePodStore(
			virtLauncherPod("ns1", "vm1", "virt-launcher-vm1-aaa"),
			virtLauncherPod("ns2", "vm2", "virt-launcher-vm2-bbb"),
		)

		c := NewCollector(Config{NodeName: "node1", CgroupRoot: "/unused", ProcPath: "/unused"}, store, nil, slog.Default())
		c.mu.Lock()
		c.vmiResults = []vmiMemStats{
			{namespace: "ns1", name: "vm1", pod: "virt-launcher-vm1-aaa", activeAnon: 100},
			{namespace: "ns2", name: "vm2", pod: "virt-launcher-vm2-bbb", activeAnon: 200},
		}
		c.mu.Unlock()

		metrics := collectMetrics(c)
		Expect(metrics["container_memory_active_anon_bytes"]).To(HaveLen(2))
	})
})
