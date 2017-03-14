JAVA_PLUGIN_INSTALLED = $(cf plugins | grep -q)

all: install

compile: cf_cli_java_plugin.go
	go build -o build/cf-cli-java-plugin cf_cli_java_plugin.go

compile-all: cf_cli_java_plugin.go
	ginkgo -p
	GOOS=linux GOARCH=386 go build -o build/cf-cli-java-plugin-linux32 cf_cli_java_plugin.go
	GOOS=linux GOARCH=amd64 go build -o build/cf-cli-java-plugin-linux64 cf_cli_java_plugin.go
	GOOS=darwin GOARCH=amd64 go build -o build/cf-cli-java-plugin-osx cf_cli_java_plugin.go
	GOOS=windows GOARCH=386 go build -o build/cf-cli-java-plugin-win32.exe cf_cli_java_plugin.go
	GOOS=windows GOARCH=amd64 go build -o build/cf-cli-java-plugin-win64.exe cf_cli_java_plugin.go

clean:
	rm -r build

install: compile remove
	yes | cf install-plugin build/cf-cli-java-plugin

remove: $(objects)
ifeq ($(JAVA_PLUGIN_INSTALLED),)
	cf uninstall-plugin JavaPlugin || true
endif

vclean: remove clean