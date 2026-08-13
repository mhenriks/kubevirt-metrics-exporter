// Package vmi provides helpers shared by collectors that map host-level
// observations (PIDs, cgroup paths, debugfs entries) back to KubeVirt VMIs.
package vmi

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ReadDomainName extracts the VM name from /proc/<pid>/cmdline by looking for
// the argument following the "-name" flag. QEMU formats it as
// "guest=<name>,debug-threads=on" or just "<name>".
func ReadDomainName(pid int, procPath string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(procPath, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", false
	}
	args := bytes.Split(data, []byte{0})
	for i, arg := range args {
		if string(arg) == "-name" && i+1 < len(args) {
			name := string(args[i+1])
			name = strings.TrimPrefix(name, "guest=")
			name = strings.SplitN(name, ",", 2)[0]
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

// ParseDomainName splits a libvirt domain name of the form "namespace_vmname"
// into its components. Kubernetes namespaces cannot contain underscores, so
// the first underscore is used as the separator.
func ParseDomainName(domain string) (ns, name string, ok bool) {
	idx := strings.Index(domain, "_")
	if idx <= 0 {
		return "", "", false
	}
	return domain[:idx], domain[idx+1:], true
}

// BuildDomainToPodMap returns a map from "namespace_vminame" to pod name for
// all running virt-launcher pods visible in the pod store.
func BuildDomainToPodMap(podStore cache.Store) map[string]string {
	m := make(map[string]string)
	for _, obj := range podStore.List() {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if pod.Labels["kubevirt.io"] != "virt-launcher" {
			continue
		}
		vmiName := pod.Labels["vm.kubevirt.io/name"]
		if vmiName == "" {
			continue
		}
		m[pod.Namespace+"_"+vmiName] = pod.Name
	}
	return m
}
