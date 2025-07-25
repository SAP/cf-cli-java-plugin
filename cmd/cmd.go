/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package cmd

// CommandExecutor is an interface that encapsulates the execution of further CF CLI commands.
// By "hiding" the CLI command execution in this interface, we can mock the command CLI execution in tests.
type CommandExecutor interface {
	Execute(args []string) ([]string, error)
}
