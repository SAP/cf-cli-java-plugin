package utils

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
)

// Version represents a semantic version with major, minor, and build numbers
type Version struct {
	Major int
	Minor int
	Build int
}

type CFAppEnv struct {
	EnvironmentVariables struct {
		JbpConfigSpringAutoReconfiguration string `json:"JBP_CONFIG_SPRING_AUTO_RECONFIGURATION"`
		JbpConfigOpenJdkJre                string `json:"JBP_CONFIG_OPEN_JDK_JRE"`
		JbpConfigComponents                string `json:"JBP_CONFIG_COMPONENTS"`
	} `json:"environment_variables"`
	StagingEnvJSON struct{} `json:"staging_env_json"`
	RunningEnvJSON struct {
		CredhubAPI string `json:"CREDHUB_API"`
	} `json:"running_env_json"`
	SystemEnvJSON struct {
		VcapServices struct {
			FsStorage []struct {
				Label          string   `json:"label"`
				Provider       any      `json:"provider"`
				Plan           string   `json:"plan"`
				Name           string   `json:"name"`
				Tags           []string `json:"tags"`
				InstanceGUID   string   `json:"instance_guid"`
				InstanceName   string   `json:"instance_name"`
				BindingGUID    string   `json:"binding_guid"`
				BindingName    any      `json:"binding_name"`
				Credentials    struct{} `json:"credentials"`
				SyslogDrainURL any      `json:"syslog_drain_url"`
				VolumeMounts   []struct {
					ContainerDir string `json:"container_dir"`
					Mode         string `json:"mode"`
					DeviceType   string `json:"device_type"`
				} `json:"volume_mounts"`
			} `json:"fs-storage"`
		} `json:"VCAP_SERVICES"`
	} `json:"system_env_json"`
	ApplicationEnvJSON struct {
		VcapApplication struct {
			CfAPI  string `json:"cf_api"`
			Limits struct {
				Fds int `json:"fds"`
			} `json:"limits"`
			ApplicationName  string   `json:"application_name"`
			ApplicationUris  []string `json:"application_uris"`
			Name             string   `json:"name"`
			SpaceName        string   `json:"space_name"`
			SpaceID          string   `json:"space_id"`
			OrganizationID   string   `json:"organization_id"`
			OrganizationName string   `json:"organization_name"`
			Uris             []string `json:"uris"`
			Users            any      `json:"users"`
			ApplicationID    string   `json:"application_id"`
		} `json:"VCAP_APPLICATION"`
	} `json:"application_env_json"`
}

// GenerateUUID generates a new RFC 4122 Version 4 UUID using Go's built-in crypto/rand
func GenerateUUID() string {
	// Generate 16 random bytes
	bytes := make([]byte, 16)
	rand.Read(bytes)

	// Set version (4) and variant bits according to RFC 4122
	bytes[6] = (bytes[6] & 0x0f) | 0x40 // Version 4
	bytes[8] = (bytes[8] & 0x3f) | 0x80 // Variant bits

	// Format as UUID string: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]))
}

func readAppEnv(app string) ([]byte, error) {
	guid, err := exec.Command("cf", "app", app, "--guid").Output()
	if err != nil {
		return nil, err
	}

	env, err := exec.Command("cf", "curl", fmt.Sprintf("/v3/apps/%s/env", strings.Trim(string(guid[:]), "\n"))).Output()
	if err != nil {
		return nil, err
	}
	return env, nil
}

func checkUserPathAvailability(app string, path string) (bool, error) {
	output, err := exec.Command("cf", "ssh", app, "-c", "[[ -d \""+path+"\" && -r \""+path+"\" && -w \""+path+"\" ]] && echo \"exists and read-writeable\"").Output()
	if err != nil {
		return false, err
	}

	if strings.Contains(string(output[:]), "exists and read-writeable") {
		return true, nil
	}

	return false, nil
}

func FindReasonForAccessError(app string) string {
	out, err := exec.Command("cf", "apps").Output()
	if err != nil {
		return "cf is not logged in, please login and try again"
	}
	// find all app names
	lines := strings.Split(string(out[:]), "\n")
	appNames := []string{}
	foundHeader := false
	for _, line := range lines {
		if foundHeader && len(line) > 0 {
			appNames = append(appNames, strings.Fields(line)[0])
		} else if strings.Contains(line, "name") && strings.Contains(line, "requested state") && strings.Contains(line, "processes") && strings.Contains(line, "routes") {
			foundHeader = true
		}
	}
	if len(appNames) == 0 {
		return "No apps in your realm, please check if you're logged in and the app exists"
	}
	if slices.Contains(appNames, app) {
		return "Problems accessing the app " + app
	}
	matches := FuzzySearch(app, appNames, 1)
	return "Could not find " + app + ". Did you mean " + matches[0] + "?"
}

func CheckRequiredTools(app string) (bool, error) {
	guid, err := exec.Command("cf", "app", app, "--guid").Output()
	if err != nil {
		return false, errors.New(FindReasonForAccessError(app))
	}
	output, err := exec.Command("cf", "curl", "/v3/apps/"+strings.TrimSuffix(string(guid), "\n")+"/ssh_enabled").Output()
	if err != nil {
		return false, err
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return false, err
	}

	if enabled, ok := result["enabled"].(bool); !ok || !enabled {
		return false, errors.New("ssh is not enabled for app: '" + app + "', please run below 2 shell commands to enable ssh and try again(please note application should be restarted before take effect):\ncf enable-ssh " + app + "\ncf restart " + app)
	}

	return true, nil
}

func GetAvailablePath(data string, userpath string) (string, error) {
	if len(userpath) > 0 {
		valid, _ := checkUserPathAvailability(data, userpath)
		if valid {
			return userpath, nil
		}

		return "", errors.New("the container path specified doesn't exist or have no read and write access, please check and try again later")
	}

	env, err := readAppEnv(data)
	if err != nil {
		return "/tmp", nil
	}

	var cfAppEnv CFAppEnv
	if err := json.Unmarshal(env, &cfAppEnv); err != nil {
		return "", err
	}

	for _, v := range cfAppEnv.SystemEnvJSON.VcapServices.FsStorage {
		for _, v2 := range v.VolumeMounts {
			if v2.Mode == "rw" {
				return v2.ContainerDir, nil
			}
		}
	}

	return "/tmp", nil
}

func CopyOverCat(args []string, src string, dest string) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return errors.New("Error creating local file at  " + dest + ". Please check that you are allowed to create files at the given local path.")
	}
	defer f.Close()

	args = append(args, "cat "+src)
	cat := exec.Command("cf", args...)

	cat.Stdout = f

	err = cat.Start()
	if err != nil {
		return errors.New("error occured during copying dump file: " + src + ", please try again.")
	}

	err = cat.Wait()
	if err != nil {
		return errors.New("error occured while waiting for the copying complete")
	}

	return nil
}

func DeleteRemoteFile(args []string, path string) error {
	args = append(args, "rm -fr "+path)
	_, err := exec.Command("cf", args...).Output()
	if err != nil {
		return errors.New("error occured while removing dump file generated")
	}

	return nil
}

func FindHeapDumpFile(args []string, fullpath string, fspath string) (string, error) {
	return FindFile(args, fullpath, fspath, "*.hprof")
}

func FindJFRFile(args []string, fullpath string, fspath string) (string, error) {
	return FindFile(args, fullpath, fspath, "*.jfr")
}

func FindFile(args []string, fullpath string, fspath string, pattern string) (string, error) {
	cmd := " [ -f '" + fullpath + "' ] && echo '" + fullpath + "' ||  find " + fspath + " -name '" + pattern + "' -printf '%T@ %p\\0' | sort -zk 1nr | sed -z 's/^[^ ]* //' | tr '\\0' '\\n' | head -n 1 "

	args = append(args, cmd)
	output, err := exec.Command("cf", args...).Output()
	if err != nil {
		// Check for SSH authentication errors
		errorStr := err.Error()
		if strings.Contains(errorStr, "Error getting one time auth code") ||
			strings.Contains(errorStr, "Error getting SSH code") ||
			strings.Contains(errorStr, "Authentication failed") {
			return "", errors.New("SSH authentication failed - this may be a Cloud Foundry platform issue. Error: " + errorStr)
		}
		return "", errors.New("error while checking the generated file: " + errorStr)
	}

	return strings.Trim(string(output[:]), "\n"), nil
}

func ListFiles(args []string, path string) ([]string, error) {
	cmd := "ls " + path
	args = append(args, cmd)
	output, err := exec.Command("cf", args...).Output()
	if err != nil {
		return nil, errors.New("error occured while listing files: " + string(output[:]))
	}
	files := strings.Split(strings.Trim(string(output[:]), "\n"), "\n")
	// filter all empty strings
	j := 0
	for _, s := range files {
		if s != "" {
			files[j] = s
			j++
		}
	}
	return files[:j], nil
}

// FuzzySearch returns up to `max` words from `words` that are closest in
// Levenshtein distance to `needle`.
func FuzzySearch(needle string, words []string, max int) []string {
	type match struct {
		distance int
		word     string
	}

	matches := make([]match, 0, len(words))
	for _, w := range words {
		matches = append(matches, match{
			distance: fuzzy.LevenshteinDistance(needle, w),
			word:     w,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].distance < matches[j].distance
	})

	if max > len(matches) {
		max = len(matches)
	}

	results := make([]string, 0, max)
	for i := range max {
		results = append(results, matches[i].word)
	}

	return results
}

// JoinWithOr joins strings with commas and "or" for the last element: "x, y, or z"
func JoinWithOr(a []string) string {
	if len(a) == 0 {
		return ""
	}
	if len(a) == 1 {
		return a[0]
	}
	return strings.Join(a[:len(a)-1], ", ") + ", or " + a[len(a)-1]
}

// WrapTextWithPrefix wraps text to fit within maxWidth characters per line,
// with the first line using the given prefix and subsequent lines indented to match the prefix length
func WrapTextWithPrefix(text, prefix string, maxWidth int, miscLineIndent int) string {
	maxDescLength := maxWidth - len(prefix)

	if len(text) <= maxDescLength {
		return prefix + text
	}

	// Split text into multiple lines if too long
	words := strings.Fields(text)
	var lines []string
	var currentLine string

	for _, word := range words {
		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		if len(testLine) <= maxDescLength {
			currentLine = testLine
		} else {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	// Join lines with proper indentation
	if len(lines) > 0 {
		result := prefix + lines[0]
		indent := strings.Repeat(" ", len(prefix))
		for i := 1; i < len(lines); i++ {
			result += "\n" + indent + strings.Repeat(" ", miscLineIndent) + lines[i]
		}
		return result
	}

	// Fallback if no lines were created
	return prefix + text
}

// ToSentenceCase converts the first character to uppercase and the rest to lowercase
func ToSentenceCase(input string) string {
	if len(input) == 0 {
		return input
	}
	// Convert the first letter to uppercase
	return strings.ToUpper(string(input[0])) + strings.ToLower(input[1:])
}
