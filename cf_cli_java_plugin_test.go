/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package main

import (
	"strings"
	"testing"

	plugin_models "code.cloudfoundry.org/cli/plugin/models"
)

const testAppName = "myapp"

// fakeConn is a stub CliConnection that knows about one app by name.
type fakeConn struct {
	knownApp string
}

func (f *fakeConn) GetApp(name string) (plugin_models.GetAppModel, error) {
	if name == f.knownApp {
		return plugin_models.GetAppModel{}, nil
	}
	return plugin_models.GetAppModel{}, &notFoundError{name}
}

type notFoundError struct{ name string }

func (e *notFoundError) Error() string { return "app not found: " + e.name }

func (f *fakeConn) CliCommandWithoutTerminalOutput(args ...string) ([]string, error) {
	return nil, nil
}
func (f *fakeConn) CliCommand(args ...string) ([]string, error) { return nil, nil }
func (f *fakeConn) GetCurrentOrg() (plugin_models.Organization, error) {
	return plugin_models.Organization{}, nil
}
func (f *fakeConn) GetCurrentSpace() (plugin_models.Space, error)       { return plugin_models.Space{}, nil }
func (f *fakeConn) Username() (string, error)                           { return "", nil }
func (f *fakeConn) UserGuid() (string, error)                           { return "", nil }
func (f *fakeConn) UserEmail() (string, error)                          { return "", nil }
func (f *fakeConn) IsLoggedIn() (bool, error)                           { return false, nil }
func (f *fakeConn) IsSSLDisabled() (bool, error)                        { return false, nil }
func (f *fakeConn) HasOrganization() (bool, error)                      { return false, nil }
func (f *fakeConn) HasSpace() (bool, error)                             { return false, nil }
func (f *fakeConn) ApiEndpoint() (string, error)                        { return "", nil }
func (f *fakeConn) ApiVersion() (string, error)                         { return "", nil }
func (f *fakeConn) HasAPIEndpoint() (bool, error)                       { return false, nil }
func (f *fakeConn) LoggregatorEndpoint() (string, error)                { return "", nil }
func (f *fakeConn) DopplerEndpoint() (string, error)                    { return "", nil }
func (f *fakeConn) AccessToken() (string, error)                        { return "", nil }
func (f *fakeConn) GetApps() ([]plugin_models.GetAppsModel, error)      { return nil, nil }
func (f *fakeConn) GetOrgs() ([]plugin_models.GetOrgs_Model, error)     { return nil, nil }
func (f *fakeConn) GetSpaces() ([]plugin_models.GetSpaces_Model, error) { return nil, nil }
func (f *fakeConn) GetOrgUsers(string, ...string) ([]plugin_models.GetOrgUsers_Model, error) {
	return nil, nil
}

func (f *fakeConn) GetSpaceUsers(string, string) ([]plugin_models.GetSpaceUsers_Model, error) {
	return nil, nil
}
func (f *fakeConn) GetServices() ([]plugin_models.GetServices_Model, error) { return nil, nil }
func (f *fakeConn) GetService(string) (plugin_models.GetService_Model, error) {
	return plugin_models.GetService_Model{}, nil
}

func (f *fakeConn) GetOrg(string) (plugin_models.GetOrg_Model, error) {
	return plugin_models.GetOrg_Model{}, nil
}

func (f *fakeConn) GetSpace(string) (plugin_models.GetSpace_Model, error) {
	return plugin_models.GetSpace_Model{}, nil
}

func TestExecute_SwappedAppAndCommand(t *testing.T) {
	p := &JavaPlugin{}
	conn := &fakeConn{knownApp: testAppName}
	// User typed: cf java myapp heap-dump  (app and command swapped)
	_, err := p.execute(conn, []string{cmdJava, testAppName, cmdHeapDump})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cf java "+cmdHeapDump+" "+testAppName) {
		t.Errorf("expected corrected command in error message, got: %s", msg)
	}
}

func TestExecute_UnrecognizedCommand_NoSwapHint_WhenAppUnknown(t *testing.T) {
	p := &JavaPlugin{}
	conn := &fakeConn{knownApp: "other-app"}
	_, err := p.execute(conn, []string{cmdJava, "bogus-command", testAppName})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "swapped") {
		t.Errorf("should not suggest swap when first arg is not a known app: %s", err.Error())
	}
}

func TestParseOptions_Open(t *testing.T) {
	p := &JavaPlugin{}
	opts, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpen})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Open {
		t.Error("expected Open=true")
	}
	if opts.OpenURL != defaultOpenURL {
		t.Errorf("expected OpenURL=%q, got %q", defaultOpenURL, opts.OpenURL)
	}
}

func TestParseOptions_OpenURL_ImpliesOpen(t *testing.T) {
	p := &JavaPlugin{}
	opts, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpenURL, "http://localhost:8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Open {
		t.Error("--open-url should imply Open=true")
	}
	if opts.OpenURL != "http://localhost:8080" {
		t.Errorf("unexpected OpenURL: %q", opts.OpenURL)
	}
}

func TestParseOptions_Open_NoDownload_Error(t *testing.T) {
	p := &JavaPlugin{}
	_, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpen, "--" + flagNoDownload})
	if err == nil {
		t.Fatal("expected error for --open + --no-download, got nil")
	}
}

func TestParseOptions_Open_DryRun_NoError(t *testing.T) {
	p := &JavaPlugin{}
	opts, _, err := p.parseOptions([]string{cmdHeapDump, testAppName, "--" + flagOpen, "--dry-run"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Open {
		t.Error("expected Open=true")
	}
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
}
