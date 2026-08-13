package cgroup

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCgroup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cgroup Suite")
}
