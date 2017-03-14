/*
 * Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package cmd

// CommandExecutor is an interface that encapsulates the execution of further cf cli commands.
// By "hiding" the cli command execution in this interface, we can mock the command cli execution in tests.
type CommandExecutor interface {
	Execute(args []string) ([]string, error)
}
