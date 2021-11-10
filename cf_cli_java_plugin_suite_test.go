package main

import (
	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	"testing"
)

func TestCfJavaPlugin(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "CfCliJavaPlugin Suite")
}
