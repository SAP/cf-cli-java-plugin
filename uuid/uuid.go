/*
 * Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package uuid

// UUIDGenerator is an interface that encapsulates the generation of UUIDs for mocking in tests.
type UUIDGenerator interface {
	Generate() string
}
