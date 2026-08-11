package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// docs/install.sh, exercised against a Docker stand-in.
//
// The behaviour worth a test is the second run. The installer's project
// directory defaults to ./3270connect relative to $PWD, and Compose names the
// project after that directory's basename — so running the one-line installer
// from somewhere else the second time lands on a second, empty ./3270connect
// that Compose still considers the same project. Left unhandled, `up -d`
// recreates the running container against a data folder with nothing in it,
// and the console comes back with no record of any run that ever happened.

// installEnv is a sandbox with a Docker stand-in on PATH.
type installEnv struct {
	t     *testing.T
	root  string
	state string
	path  string
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the installer is a Linux shell script")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}

	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("create stub bin: %v", err)
	}

	fake, err := os.ReadFile(filepath.Join("testdata", "fake-docker.sh"))
	if err != nil {
		t.Fatalf("read fake docker: %v", err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	write("docker", string(fake))
	// The health check is the only thing the compose path needs curl for.
	write("curl", "#!/usr/bin/env bash\nexit 0\n")
	// Shadow sudo so prepare_data_dir cannot hand the sandbox to uid 10001 and
	// leave the test unable to clean up after itself. The installer treats a
	// failed chown as a warning, which is the path a non-root run takes anyway.
	write("sudo", "#!/usr/bin/env bash\nexit 0\n")

	return &installEnv{
		t:     t,
		root:  root,
		state: filepath.Join(root, "docker-state"),
		path:  bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
}

// dir makes a working directory to run the installer from.
func (e *installEnv) dir(name string) string {
	e.t.Helper()
	path := filepath.Join(e.root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		e.t.Fatalf("create %s: %v", path, err)
	}
	return path
}

// run executes the installer from workdir and returns its combined output.
func (e *installEnv) run(workdir string, args ...string) string {
	e.t.Helper()

	script, err := filepath.Abs(filepath.Join("docs", "install.sh"))
	if err != nil {
		e.t.Fatalf("resolve install.sh: %v", err)
	}

	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"PATH="+e.path,
		"FAKE_DOCKER_STATE="+e.state,
		"HOME="+e.root,
		"NO_COLOR=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("installer failed: %v\n%s", err, out)
	}
	return string(out)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInstallerComposeWritesAConsoleStack(t *testing.T) {
	env := newInstallEnv(t)
	work := env.dir("first")

	env.run(work, "--method", "compose", "--yes")

	stack := filepath.Join(work, "3270connect", "docker-compose.yml")
	body := readFile(t, stack)

	for _, want := range []string{
		"ghcr.io/3270io/3270connect:latest",
		"container_name: 3270connect",
		// The console binds localhost by default, which inside a container is
		// unreachable through a published port. The stack has to say so.
		"DASHBOARD_BIND=0.0.0.0",
		"./data:/data",
		`"127.0.0.1:9200:9200"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("generated stack does not contain %q:\n%s", want, body)
		}
	}

	if _, err := os.Stat(filepath.Join(work, "3270connect", "data")); err != nil {
		t.Errorf("data folder was not created: %v", err)
	}
}

// The one that matters: a second run from a different directory must update
// the install that is already here, against the data folder it is already
// using.
func TestInstallerRerunFromElsewhereKeepsTheDataFolder(t *testing.T) {
	env := newInstallEnv(t)

	first := env.dir("first")
	env.run(first, "--method", "compose", "--yes")

	elsewhere := env.dir("elsewhere")
	out := env.run(elsewhere, "--method", "compose", "--yes")

	if !strings.Contains(out, "already running from") {
		t.Errorf("the second run did not report finding the first install:\n%s", out)
	}

	// No second stack beside the directory the second run happened to start in.
	if _, err := os.Stat(filepath.Join(elsewhere, "3270connect", "docker-compose.yml")); err == nil {
		t.Error("a second stack was written in the directory the re-run started from")
	}

	stack := filepath.Join(first, "3270connect", "docker-compose.yml")
	body := readFile(t, stack)
	if !strings.Contains(body, ":/data") {
		t.Fatalf("the original stack lost its data mount:\n%s", body)
	}
	// Carried forward as the running container reports it, which is resolved.
	if !strings.Contains(body, filepath.Join(first, "3270connect", "data")+":/data") &&
		!strings.Contains(body, "./data:/data") {
		t.Errorf("the re-run repointed /data somewhere else:\n%s", body)
	}
}

// A port given on the command line wins; a port not given is whatever the
// install is already published on.
func TestInstallerRerunKeepsThePortItWasGiven(t *testing.T) {
	env := newInstallEnv(t)
	work := env.dir("first")

	env.run(work, "--method", "compose", "--yes", "--port", "9300")
	stack := filepath.Join(work, "3270connect", "docker-compose.yml")
	if body := readFile(t, stack); !strings.Contains(body, "9300:9200") {
		t.Fatalf("the first run did not publish on 9300:\n%s", body)
	}

	env.run(work, "--method", "compose", "--yes")
	if body := readFile(t, stack); !strings.Contains(body, "9300:9200") {
		t.Errorf("the re-run reset the port to the default:\n%s", body)
	}
}

func TestInstallerLabWritesThreeServicesAndAWorkflow(t *testing.T) {
	env := newInstallEnv(t)
	work := env.dir("lab")

	env.run(work, "--method", "lab", "--yes")

	stack := filepath.Join(work, "3270-lab", "docker-compose.yml")
	body := readFile(t, stack)

	for _, want := range []string{
		"sampleapps:",
		"3270web:",
		"3270connect:",
		"ghcr.io/3270io/3270web:latest",
		"ghcr.io/3270io/3270connect:latest",
		// The sample host serves no HTTP, so the image's own healthcheck
		// cannot be left in place.
		"/dev/tcp/127.0.0.1/3271",
		"workflow-sampleapp.json:/data/workflow-sampleapp.json:ro",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("generated lab stack does not contain %q:\n%s", want, body)
		}
	}

	workflow := readFile(t, filepath.Join(work, "3270-lab", "workflow-sampleapp.json"))
	if !strings.Contains(workflow, `"Host": "sampleapps"`) {
		t.Errorf("the lab workflow is not pointed at the sample host:\n%s", workflow)
	}
	// It has to be a workflow this build can actually run, not just JSON: an
	// example that fails validation is worse than no example.
	var config Configuration
	if err := json.Unmarshal([]byte(workflow), &config); err != nil {
		t.Fatalf("the lab workflow is not valid JSON: %v", err)
	}
	if err := validateConfiguration(&config); err != nil {
		t.Errorf("the lab workflow does not validate: %v", err)
	}
}

// The workflow the lab stack in this repository mounts is the first one most
// people will run. It is checked here for the same reason the generated one
// is: an example that does not validate teaches the wrong shape.
func TestRepositoryLabWorkflowValidates(t *testing.T) {
	body := readFile(t, filepath.Join("lab", "workflow-sampleapp.json"))

	var config Configuration
	if err := json.Unmarshal([]byte(body), &config); err != nil {
		t.Fatalf("lab/workflow-sampleapp.json is not valid JSON: %v", err)
	}
	if err := validateConfiguration(&config); err != nil {
		t.Errorf("lab/workflow-sampleapp.json does not validate: %v", err)
	}
	// The host is the service name in docker-compose.lab.yml, and the port is
	// the one that file serves app1 on. Either drifting breaks the lab.
	if config.Host != "sampleapps" || config.Port != 3272 {
		t.Errorf("workflow targets %s:%d, want sampleapps:3272 (the lab stack's app1)", config.Host, config.Port)
	}

	stack := readFile(t, "docker-compose.lab.yml")
	if !strings.Contains(stack, "app1:3272") {
		t.Errorf("docker-compose.lab.yml no longer serves app1 on 3272, so the workflow points at nothing")
	}
}

func TestInstallerDryRunWritesNothing(t *testing.T) {
	env := newInstallEnv(t)
	work := env.dir("dry")

	env.run(work, "--method", "compose", "--yes", "--dry-run")

	if _, err := os.Stat(filepath.Join(work, "3270connect")); err == nil {
		t.Error("--dry-run created the project directory")
	}
}
