/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

// This file was generated by counterfeiter
package fakes

import (
	"sync"

	"github.com/SAP/cf-cli-java-plugin/uuid"
)

type FakeUUIDGenerator struct {
	GenerateStub        func() string
	generateMutex       sync.RWMutex
	generateArgsForCall []struct{}
	generateReturns     struct {
		result1 string
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeUUIDGenerator) Generate() string {
	fake.generateMutex.Lock()
	fake.generateArgsForCall = append(fake.generateArgsForCall, struct{}{})
	fake.recordInvocation("Generate", []interface{}{})
	fake.generateMutex.Unlock()
	if fake.GenerateStub != nil {
		return fake.GenerateStub()
	}
	return fake.generateReturns.result1
}

func (fake *FakeUUIDGenerator) GenerateCallCount() int {
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	return len(fake.generateArgsForCall)
}

func (fake *FakeUUIDGenerator) GenerateReturns(result1 string) {
	fake.GenerateStub = nil
	fake.generateReturns = struct {
		result1 string
	}{result1}
}

func (fake *FakeUUIDGenerator) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	return fake.invocations
}

func (fake *FakeUUIDGenerator) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ uuid.UUIDGenerator = new(FakeUUIDGenerator)
