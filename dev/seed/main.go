package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/atm1020/gojenkins"
)

type jobDef struct {
	name    string
	parents []string
	config  string
	builds  []map[string]string // nil entry = no params
	desc    string
}

func pipelineXML(desc, script string) string {
	return fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?>
<flow-definition plugin="workflow-job">
  <description>%s</description>
  <definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition" plugin="workflow-cps">
    <script>%s</script>
    <sandbox>true</sandbox>
  </definition>
</flow-definition>`, xmlEscape(desc), xmlEscape(script))
}

func freestyleXML(desc, shell string) string {
	return fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?>
<project>
  <description>%s</description>
  <builders>
    <hudson.tasks.Shell>
      <command>%s</command>
    </hudson.tasks.Shell>
  </builders>
</project>`, xmlEscape(desc), xmlEscape(shell))
}

// paramDef describes a single Jenkins parameter definition.
type paramDef struct {
	xmlTag string // e.g. "hudson.model.StringParameterDefinition"
	body   string // inner XML elements (name, defaultValue, choices, etc.)
}

func stringParam(name, defaultVal, description string) paramDef {
	return paramDef{
		xmlTag: "hudson.model.StringParameterDefinition",
		body: fmt.Sprintf("<name>%s</name><defaultValue>%s</defaultValue><description>%s</description>",
			name, xmlEscape(defaultVal), xmlEscape(description)),
	}
}

func boolParam(name string, defaultVal bool, description string) paramDef {
	return paramDef{
		xmlTag: "hudson.model.BooleanParameterDefinition",
		body: fmt.Sprintf("<name>%s</name><defaultValue>%t</defaultValue><description>%s</description>",
			name, defaultVal, xmlEscape(description)),
	}
}

func choiceParam(name string, choices []string, description string) paramDef {
	var cb strings.Builder
	cb.WriteString("<a class=\"string-array\">")
	for _, c := range choices {
		cb.WriteString("<string>" + xmlEscape(c) + "</string>")
	}
	cb.WriteString("</a>")
	return paramDef{
		xmlTag: "hudson.model.ChoiceParameterDefinition",
		body:   fmt.Sprintf(`<name>%s</name><choices class="java.util.Arrays$ArrayList">%s</choices><description>%s</description>`, name, cb.String(), xmlEscape(description)),
	}
}

// activeChoiceParam and reactiveChoiceParam are the single-select shorthands;
// unoChoice in dynamicjobs.go builds the XML for every Active Choices type.
func activeChoiceParam(name, description, script string) paramDef {
	return unoChoice(name, description, script, unoChoiceOpts{})
}

func reactiveChoiceParam(name, description, referencedParameters, script string) paramDef {
	return unoChoice(name, description, script, unoChoiceOpts{referenced: referencedParameters})
}

func textParam(name, defaultVal, description string) paramDef {
	return paramDef{
		xmlTag: "hudson.model.TextParameterDefinition",
		body: fmt.Sprintf("<name>%s</name><defaultValue>%s</defaultValue><description>%s</description>",
			name, xmlEscape(defaultVal), xmlEscape(description)),
	}
}

func passwordParam(name, defaultVal, description string) paramDef {
	return paramDef{
		xmlTag: "hudson.model.PasswordParameterDefinition",
		body: fmt.Sprintf("<name>%s</name><defaultValue>%s</defaultValue><description>%s</description>",
			name, xmlEscape(defaultVal), xmlEscape(description)),
	}
}

func multiParamPipelineXML(desc string, params []paramDef, script string) string {
	var pb strings.Builder
	for _, p := range params {
		fmt.Fprintf(&pb, "<%s>%s</%s>\n", p.xmlTag, p.body, p.xmlTag)
	}
	return fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?>
<flow-definition plugin="workflow-job">
  <description>%s</description>
  <properties>
    <hudson.model.ParametersDefinitionProperty>
      <parameterDefinitions>
        %s
      </parameterDefinitions>
    </hudson.model.ParametersDefinitionProperty>
  </properties>
  <definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition" plugin="workflow-cps">
    <script>%s</script>
    <sandbox>true</sandbox>
  </definition>
</flow-definition>`, xmlEscape(desc), pb.String(), xmlEscape(script))
}

// Convenience wrapper for single-param jobs (keeps existing call sites working).
func paramPipelineXML(desc, paramName, defaultVal, script string) string {
	return multiParamPipelineXML(desc, []paramDef{stringParam(paramName, defaultVal, "")}, script)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// onlyFilter, when non-empty, restricts seeding to job paths containing it.
var onlyFilter string

func main() {
	ctx := context.Background()

	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	urlFlag := fs.String("url", envOr("JENKINS_URL", "http://localhost:8080"), "Jenkins server URL")
	userFlag := fs.String("user", envOr("JENKINS_USER", "admin"), "Jenkins username")
	passFlag := fs.String("pass", envOr("JENKINS_PASS", "admin"), "Jenkins password or API token")
	onlyFlag := fs.String("only", "", "Seed only jobs whose full path contains this substring")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: seed [flags] [target]\n\n")
		fmt.Fprintf(fs.Output(), "Targets: users, jobs (omit to seed everything)\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	onlyFilter = *onlyFlag

	fmt.Printf("Connecting to Jenkins at %s...\n", *urlFlag)
	jenkins := gojenkins.CreateJenkins(nil, *urlFlag, *userFlag, *passFlag)
	_, err := jenkins.Init(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Jenkins: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connected.")

	switch target {
	case "users":
		seedUsers(ctx, *urlFlag, *userFlag, *passFlag)
	case "jobs":
		if err := seedFolders(ctx, jenkins); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		seedJobs(ctx, jenkins)
	default:
		seedUsers(ctx, *urlFlag, *userFlag, *passFlag)
		if err := seedFolders(ctx, jenkins); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		seedJobs(ctx, jenkins)
	}

	fmt.Println("\nSeed complete!")
}

func seedUsers(ctx context.Context, baseURL, user, pass string) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Fetch crumb with a session cookie so /scriptText accepts the request.
	crumbURL := baseURL + "/crumbIssuer/api/json"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, crumbURL, nil)
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  Warning: failed to get crumb: %v\n", err)
		return
	}

	var crumbData struct {
		Crumb             string `json:"crumb"`
		CrumbRequestField string `json:"crumbRequestField"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&crumbData); err != nil {
		resp.Body.Close()
		fmt.Printf("  Warning: failed to parse crumb: %v\n", err)
		return
	}
	resp.Body.Close()

	users := []struct {
		username, password string
	}{
		{"admin", "admin"},
		{"developer", "developer"},
		{"qa", "qa"},
		{"ops", "ops"},
	}

	for _, u := range users {
		fmt.Printf("Creating user: %s\n", u.username)
		script := fmt.Sprintf(
			`def realm = jenkins.model.Jenkins.instance.securityRealm
realm.createAccount("%s", "%s")
println("created")`,
			u.username, u.password,
		)
		payload := url.Values{"script": {script}}.Encode()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/scriptText", strings.NewReader(payload))
		req.SetBasicAuth(user, pass)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(crumbData.CrumbRequestField, crumbData.Crumb)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  Warning: %v\n", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			fmt.Printf("  Warning: script console returned %d\n", resp.StatusCode)
		} else {
			fmt.Printf("  OK: %s\n", strings.TrimSpace(string(body)))
		}
	}
}

func seedFolders(ctx context.Context, jenkins *gojenkins.Jenkins) error {
	folders := []struct {
		name    string
		parents []string
	}{
		{"DevTeam", nil},
		{"Backend", []string{"DevTeam"}},
		{"Frontend", []string{"DevTeam"}},
		{"Ops", nil},
		{"Monitoring", []string{"Ops"}},
		{"Deployments", []string{"Ops"}},
		{"QA", nil},
		{"Dynamic", nil},
	}

	for _, f := range folders {
		fmt.Printf("Creating folder: %s (parents: %v)\n", f.name, f.parents)
		if _, err := jenkins.GetFolder(ctx, f.name, f.parents...); err == nil {
			fmt.Println("  Folder exists.")
			continue
		}
		if _, err := jenkins.CreateFolder(ctx, f.name, f.parents...); err != nil {
			// Jenkins redirects successful folder creation to an HTML page, which
			// gojenkins attempts to decode as JSON. Check the actual side effect.
			if _, lookupErr := jenkins.GetFolder(ctx, f.name, f.parents...); lookupErr == nil {
				continue
			}
			return fmt.Errorf("creating folder %s: %w (ensure the cloudbees-folder plugin is installed; rebuild local Jenkins with docker compose -f dev/docker/docker-compose.yaml up -d --build jenkins-lts)", f.name, err)
		}
	}
	return nil
}

func seedJobs(ctx context.Context, jenkins *gojenkins.Jenkins) {
	jobs := []jobDef{
		{
			name:    "api-service",
			parents: []string{"DevTeam", "Backend"},
			desc:    "Backend API service build pipeline",
			config: pipelineXML("Backend API service build pipeline", `
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Compiling API service...'
                sh 'sleep 3'
                echo 'Build complete.'
            }
        }
        stage('Test') {
            steps {
                echo 'Running unit tests...'
                sh 'sleep 5'
                echo 'All 142 tests passed.'
            }
        }
        stage('Deploy') {
            steps {
                echo 'Deploying to staging environment...'
                sh 'sleep 2'
                echo 'Deployment successful.'
            }
        }
    }
}`),
			builds: []map[string]string{nil, nil, nil},
		},
		{
			name:    "log-heavy-job",
			parents: []string{"DevTeam", "Backend"},
			desc:    "Generates large log output for testing log viewer",
			config:  pipelineXML("Generates large log output for testing log viewer", logHeavyScript()),
			builds:  []map[string]string{nil},
		},
		{
			name:    "ansi-color-log",
			parents: []string{"DevTeam", "Backend"},
			desc:    "Emits ANSI-colored log lines for testing the TUI log viewer",
			config:  pipelineXML("Emits ANSI-colored log lines for testing the TUI log viewer", ansiColorScript()),
			builds:  []map[string]string{nil},
		},
		{
			name:    "flaky-tests",
			parents: []string{"DevTeam", "Backend"},
			desc:    "Simulates flaky test failures",
			config: pipelineXML("Simulates flaky test failures", `
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Building project...'
                sh 'sleep 2'
            }
        }
        stage('Test') {
            steps {
                echo 'Running test suite...'
                sh 'sleep 3'
                echo 'FAILURE: TestUserAuth - expected 200, got 503'
                echo 'FAILURE: TestPaymentFlow - timeout after 30s'
                sh 'exit 1'
            }
        }
        stage('Deploy') {
            steps {
                echo 'This stage should not run.'
            }
        }
    }
}`),
			builds: []map[string]string{nil, nil},
		},
		{
			name:    "webapp-build",
			parents: []string{"DevTeam", "Frontend"},
			desc:    "Frontend web application with parallel test stages",
			config: pipelineXML("Frontend web application with parallel test stages", `
pipeline {
    agent any
    stages {
        stage('Install') {
            steps {
                echo 'Installing dependencies...'
                sh 'sleep 2'
            }
        }
        stage('Parallel Tests') {
            parallel {
                stage('Unit') {
                    steps {
                        echo 'Running unit tests...'
                        sh 'sleep 4'
                        echo '89 tests passed.'
                    }
                }
                stage('Integration') {
                    steps {
                        echo 'Running integration tests...'
                        sh 'sleep 6'
                        echo '23 tests passed.'
                    }
                }
                stage('Lint') {
                    steps {
                        echo 'Running eslint...'
                        sh 'sleep 3'
                        echo 'No linting errors found.'
                    }
                }
            }
        }
        stage('Bundle') {
            steps {
                echo 'Creating production bundle...'
                sh 'sleep 3'
                echo 'Bundle size: 1.2MB'
            }
        }
    }
}`),
			builds: []map[string]string{nil, nil},
		},
		{
			name:    "mobile-app",
			parents: []string{"DevTeam", "Frontend"},
			desc:    "Mobile app pipeline with many stages for wide stage view",
			config: pipelineXML("Mobile app pipeline with many stages for wide stage view", `
pipeline {
    agent any
    stages {
        stage('Checkout') {
            steps { echo 'Checking out source...'; sh 'sleep 1' }
        }
        stage('Dependencies') {
            steps { echo 'Installing pods/gradle deps...'; sh 'sleep 2' }
        }
        stage('Lint') {
            steps { echo 'Running SwiftLint/ktlint...'; sh 'sleep 2' }
        }
        stage('Unit Tests') {
            steps { echo 'Running unit tests...'; sh 'sleep 3' }
        }
        stage('Build iOS') {
            steps { echo 'Building iOS target...'; sh 'sleep 4' }
        }
        stage('Build Android') {
            steps { echo 'Building Android APK...'; sh 'sleep 4' }
        }
        stage('UI Tests') {
            steps { echo 'Running UI automation...'; sh 'sleep 5' }
        }
        stage('Publish') {
            steps { echo 'Uploading to TestFlight/Play Console...'; sh 'sleep 2' }
        }
    }
}`),
			builds: []map[string]string{nil, nil},
		},
		{
			name:    "long-running-scan",
			parents: []string{"Ops", "Monitoring"},
			desc:    "Long-running security scan (stays in-progress for testing pulse animation)",
			config: pipelineXML("Long-running security scan", `
pipeline {
    agent any
    stages {
        stage('Scan') {
            steps {
                echo 'Starting deep security scan...'
                sh 'sleep 300'
                echo 'Scan complete.'
            }
        }
    }
}`),
			builds: []map[string]string{nil},
		},
		{
			name:    "deploy-service",
			parents: []string{"Ops", "Deployments"},
			desc:    "Parameterized deployment pipeline",
			config: paramPipelineXML("Parameterized deployment pipeline", "ENVIRONMENT", "staging", `
pipeline {
    agent any
    stages {
        stage('Validate') {
            steps {
                echo "Validating deployment to ${params.ENVIRONMENT}..."
                sh 'sleep 2'
            }
        }
        stage('Deploy') {
            steps {
                echo "Deploying to ${params.ENVIRONMENT}..."
                sh 'sleep 5'
                echo "Deployment to ${params.ENVIRONMENT} complete."
            }
        }
        stage('Verify') {
            steps {
                echo "Running smoke tests on ${params.ENVIRONMENT}..."
                sh 'sleep 3'
                echo 'All health checks passed.'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"ENVIRONMENT": "staging"},
				{"ENVIRONMENT": "production"},
			},
		},
		{
			name:    "approval-gated-deploy",
			parents: []string{"Ops", "Deployments"},
			desc:    "Deployment pipeline that waits for manual approval",
			config: pipelineXML("Deployment pipeline that waits for manual approval", `
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Building deployment artifact...'
                sh 'sleep 2'
            }
        }
        stage('Approval') {
            steps {
                input message: 'Deploy to production?', ok: 'Approve'
            }
        }
        stage('Deploy') {
            steps {
                echo 'Deploying approved artifact to production...'
                sh 'sleep 3'
                echo 'Deployment successful.'
            }
        }
    }
}`),
			builds: []map[string]string{nil},
		},
		{
			name:    "dynamic-deploy-active-choices",
			parents: []string{"Ops", "Deployments"},
			desc:    "Deployment pipeline using Active Choices dynamic parameters",
			config: multiParamPipelineXML("Deployment pipeline using Active Choices dynamic parameters",
				[]paramDef{
					activeChoiceParam("ENV", "Select deployment environment", "return ['dev', 'staging', 'production']"),
					reactiveChoiceParam("SERVER", "Available servers for selected environment", "ENV", `
if (ENV == 'dev') {
    return ['dev-app-1', 'dev-app-2']
}
if (ENV == 'staging') {
    return ['staging-app-1']
}
if (ENV == 'production') {
    return ['prod-app-1', 'prod-app-2', 'prod-app-3']
}
return ['unknown']`),
					boolParam("DRY_RUN", true, "Preview deployment without making changes"),
				}, `
pipeline {
    agent any
    stages {
        stage('Validate') {
            steps {
                echo "Environment: ${params.ENV}"
                echo "Server: ${params.SERVER}"
                echo "Dry run: ${params.DRY_RUN}"
                sh 'sleep 1'
            }
        }
        stage('Deploy') {
            steps {
                script {
                    if (params.DRY_RUN) {
                        echo "[DRY RUN] Would deploy to ${params.SERVER} (${params.ENV})"
                    } else {
                        echo "Deploying to ${params.SERVER} (${params.ENV})..."
                        sh 'sleep 3'
                        echo 'Deployment successful.'
                    }
                }
            }
        }
    }
}`),
			builds: []map[string]string{
				{"ENV": "dev", "SERVER": "dev-app-1", "DRY_RUN": "true"},
				{"ENV": "production", "SERVER": "prod-app-2", "DRY_RUN": "false"},
			},
		},
		{
			name:    "smoke-tests",
			parents: []string{"QA"},
			desc:    "Quick smoke test suite with long build history",
			config: pipelineXML("Quick smoke test suite", `
pipeline {
    agent any
    stages {
        stage('Smoke') {
            steps {
                echo 'Running smoke tests...'
                sh 'sleep 3'
                echo 'All smoke tests passed.'
            }
        }
    }
}`),
			builds: []map[string]string{nil, nil, nil, nil, nil},
		},
		{
			name:    "artifact-generator",
			parents: []string{"DevTeam", "Backend"},
			desc:    "Pipeline that generates downloadable artifacts",
			config: pipelineXML("Pipeline that generates downloadable artifacts", `
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Compiling project...'
                sh 'sleep 2'
                echo 'Build complete.'
            }
        }
        stage('Generate Artifacts') {
            steps {
                echo 'Generating build artifacts...'
                sh '''
                    mkdir -p build/reports build/dist

                    echo '{"status":"success","tests":142,"passed":140,"failed":2,"skipped":0,"duration":"3m42s"}' > build/reports/test-results.json

                    printf 'Module,Coverage\nauth,94.2\napi,87.5\ndb,91.0\nutils,78.3\nhandlers,85.1\n' > build/reports/coverage.csv

                    cat > build/reports/summary.html <<ENDHTML
<html><body>
<h1>Build Report #${BUILD_NUMBER}</h1>
<p>Generated: $(date)</p>
<h2>Test Results</h2>
<table border="1">
<tr><th>Suite</th><th>Pass</th><th>Fail</th></tr>
<tr><td>Unit</td><td>98</td><td>1</td></tr>
<tr><td>Integration</td><td>32</td><td>0</td></tr>
<tr><td>E2E</td><td>10</td><td>1</td></tr>
</table>
</body></html>
ENDHTML

                    dd if=/dev/urandom bs=1024 count=512 2>/dev/null | base64 > build/dist/app-binary.dat
                    tar czf build/dist/app-bundle.tar.gz -C build/reports .

                    echo "=== Artifacts Generated ==="
                    ls -lhR build/
                '''
            }
        }
        stage('Archive') {
            steps {
                archiveArtifacts artifacts: 'build/**/*', fingerprint: true
                echo 'Artifacts archived successfully.'
            }
        }
    }
}`),
			builds: []map[string]string{nil, nil},
		},
		// --- Parameterized jobs covering all parameter types ---
		{
			name:    "release-pipeline",
			parents: []string{"Ops", "Deployments"},
			desc:    "Release pipeline with string + boolean + choice params",
			config: multiParamPipelineXML("Release pipeline with mixed parameters",
				[]paramDef{
					stringParam("VERSION", "1.0.0", "Semantic version to release"),
					boolParam("DRY_RUN", true, "If true, skip actual publish step"),
					choiceParam("TARGET", []string{"docker", "npm", "pypi", "github-release"}, "Package registry to publish to"),
				}, `
pipeline {
    agent any
    stages {
        stage('Validate') {
            steps {
                echo "Preparing release v${params.VERSION}"
                echo "Target registry: ${params.TARGET}"
                echo "Dry run: ${params.DRY_RUN}"
                sh 'sleep 2'
            }
        }
        stage('Build') {
            steps {
                echo "Building release artifacts for v${params.VERSION}..."
                sh 'sleep 3'
            }
        }
        stage('Publish') {
            steps {
                script {
                    if (params.DRY_RUN) {
                        echo "[DRY RUN] Would publish v${params.VERSION} to ${params.TARGET}"
                    } else {
                        echo "Publishing v${params.VERSION} to ${params.TARGET}..."
                        sh 'sleep 2'
                        echo 'Published successfully.'
                    }
                }
            }
        }
    }
}`),
			builds: []map[string]string{
				{"VERSION": "1.0.0", "DRY_RUN": "true", "TARGET": "docker"},
				{"VERSION": "2.0.0-rc1", "DRY_RUN": "false", "TARGET": "npm"},
			},
		},
		{
			name:    "run-script",
			parents: []string{"Ops", "Monitoring"},
			desc:    "Job with text (multiline) + password + string params",
			config: multiParamPipelineXML("Run ad-hoc script with credentials",
				[]paramDef{
					textParam("SCRIPT_BODY", "echo 'hello world'\ndate\nuname -a", "Shell script to execute (multiline)"),
					passwordParam("API_TOKEN", "", "API token for authenticated commands"),
					stringParam("TARGET_HOST", "app-server-01.internal", "Host to run the script on"),
				}, `
pipeline {
    agent any
    stages {
        stage('Prepare') {
            steps {
                echo "Target host: ${params.TARGET_HOST}"
                echo "Script length: ${params.SCRIPT_BODY.length()} chars"
                echo 'Token is masked by Jenkins.'
                sh 'sleep 1'
            }
        }
        stage('Execute') {
            steps {
                echo '--- Script Output ---'
                sh 'sleep 3'
                echo 'Script executed successfully.'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"TARGET_HOST": "app-server-01.internal", "SCRIPT_BODY": "echo running\nps aux\ndf -h"},
				{"TARGET_HOST": "db-server-02.internal", "SCRIPT_BODY": "echo checking db\nmysql --version"},
			},
		},
		{
			name:    "integration-suite",
			parents: []string{"QA"},
			desc:    "Test suite with choice + boolean + string params",
			config: multiParamPipelineXML("Configurable integration test suite",
				[]paramDef{
					choiceParam("BROWSER", []string{"chrome", "firefox", "safari", "edge"}, "Browser to run tests in"),
					choiceParam("TEST_SUITE", []string{"smoke", "regression", "full", "performance"}, "Which test suite to run"),
					boolParam("HEADLESS", true, "Run browser in headless mode"),
					stringParam("BASE_URL", "https://staging.example.com", "Base URL of the application under test"),
					boolParam("RECORD_VIDEO", false, "Record video of test execution"),
				}, `
pipeline {
    agent any
    stages {
        stage('Setup') {
            steps {
                echo "Browser: ${params.BROWSER}"
                echo "Suite: ${params.TEST_SUITE}"
                echo "Headless: ${params.HEADLESS}"
                echo "Base URL: ${params.BASE_URL}"
                echo "Record video: ${params.RECORD_VIDEO}"
                sh 'sleep 2'
            }
        }
        stage('Run Tests') {
            steps {
                echo "Launching ${params.TEST_SUITE} tests in ${params.BROWSER}..."
                sh 'sleep 5'
                echo '47 tests passed, 2 skipped, 0 failed.'
            }
        }
        stage('Report') {
            steps {
                echo 'Generating test report...'
                sh 'sleep 1'
                echo 'Report available at /reports/latest'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"BROWSER": "chrome", "TEST_SUITE": "smoke", "HEADLESS": "true", "BASE_URL": "https://staging.example.com", "RECORD_VIDEO": "false"},
				{"BROWSER": "firefox", "TEST_SUITE": "regression", "HEADLESS": "false", "BASE_URL": "https://qa.example.com", "RECORD_VIDEO": "true"},
				{"BROWSER": "safari", "TEST_SUITE": "full", "HEADLESS": "true", "BASE_URL": "https://staging.example.com", "RECORD_VIDEO": "false"},
			},
		},
		{
			name:    "db-migration",
			parents: []string{"Ops", "Deployments"},
			desc:    "Database migration with text + choice + boolean + password params",
			config: multiParamPipelineXML("Database migration runner",
				[]paramDef{
					choiceParam("DATABASE", []string{"postgres", "mysql", "mongodb"}, "Target database engine"),
					stringParam("DB_HOST", "db-primary.internal:5432", "Database host and port"),
					passwordParam("DB_PASSWORD", "", "Database admin password"),
					textParam("MIGRATION_SQL", "-- Add your migration SQL here\nALTER TABLE users ADD COLUMN last_login TIMESTAMP;", "SQL migration script to execute"),
					boolParam("BACKUP_FIRST", true, "Create a backup before running migration"),
					boolParam("ROLLBACK_ON_ERROR", true, "Automatically rollback if migration fails"),
				}, `
pipeline {
    agent any
    stages {
        stage('Pre-flight') {
            steps {
                echo "Database: ${params.DATABASE} at ${params.DB_HOST}"
                echo "Backup first: ${params.BACKUP_FIRST}"
                echo "Auto-rollback: ${params.ROLLBACK_ON_ERROR}"
                echo "Migration SQL length: ${params.MIGRATION_SQL.length()} chars"
                sh 'sleep 1'
            }
        }
        stage('Backup') {
            when { expression { params.BACKUP_FIRST } }
            steps {
                echo "Creating backup of ${params.DATABASE}..."
                sh 'sleep 3'
                echo 'Backup completed: backup_20240315_143022.sql.gz'
            }
        }
        stage('Migrate') {
            steps {
                echo 'Running migration...'
                sh 'sleep 4'
                echo 'Migration applied successfully. 1 table altered.'
            }
        }
        stage('Verify') {
            steps {
                echo 'Running post-migration checks...'
                sh 'sleep 2'
                echo 'Schema validation passed.'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"DATABASE": "postgres", "DB_HOST": "db-primary.internal:5432", "BACKUP_FIRST": "true", "ROLLBACK_ON_ERROR": "true", "MIGRATION_SQL": "ALTER TABLE users ADD COLUMN last_login TIMESTAMP;"},
				{"DATABASE": "mysql", "DB_HOST": "mysql-prod.internal:3306", "BACKUP_FIRST": "true", "ROLLBACK_ON_ERROR": "false", "MIGRATION_SQL": "CREATE INDEX idx_email ON users(email);"},
			},
		},
		{
			name:    "all-param-types",
			parents: []string{"QA"},
			desc:    "Job exercising every supported parameter type in a single definition",
			config: multiParamPipelineXML("All parameter types demo",
				[]paramDef{
					stringParam("APP_NAME", "my-service", "Application name"),
					textParam("CHANGELOG", "- Fixed login bug\n- Updated dependencies\n- Improved performance", "Multiline changelog"),
					boolParam("NOTIFY_SLACK", true, "Send Slack notification on completion"),
					choiceParam("REGION", []string{"us-east-1", "eu-west-1", "ap-southeast-1"}, "AWS region to deploy to"),
					passwordParam("SECRET_KEY", "", "Secret key for signing artifacts"),
				}, `
pipeline {
    agent any
    stages {
        stage('Summary') {
            steps {
                echo "App: ${params.APP_NAME}"
                echo "Region: ${params.REGION}"
                echo "Notify Slack: ${params.NOTIFY_SLACK}"
                echo "Changelog length: ${params.CHANGELOG.length()} chars"
                echo 'Secret key is masked.'
                sh 'sleep 2'
            }
        }
        stage('Build') {
            steps {
                echo "Building ${params.APP_NAME} for ${params.REGION}..."
                sh 'sleep 3'
                echo 'Build complete.'
            }
        }
        stage('Deploy') {
            steps {
                echo "Deploying to ${params.REGION}..."
                sh 'sleep 2'
                echo 'Deployment successful.'
            }
        }
        stage('Notify') {
            when { expression { params.NOTIFY_SLACK } }
            steps {
                echo "Sending Slack notification..."
                sh 'sleep 1'
                echo 'Notification sent.'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"APP_NAME": "my-service", "REGION": "us-east-1", "NOTIFY_SLACK": "true", "CHANGELOG": "- Fixed login bug\n- Updated deps"},
				{"APP_NAME": "auth-svc", "REGION": "eu-west-1", "NOTIFY_SLACK": "false", "CHANGELOG": "- New OAuth2 flow"},
			},
		},
		{
			name:    "quick-health-check",
			parents: nil, // root level
			desc:    "Simple freestyle health check",
			config:  freestyleXML("Simple freestyle health check", "echo 'Health check OK'\nsleep 1\necho 'All systems operational'"),
			builds:  []map[string]string{nil, nil, nil},
		},
	}

	// Active Choices (uno-choice) fixtures live in dynamicjobs.go.
	jobs = append(jobs, dynamicChoiceJobs()...)

	for _, j := range jobs {
		path := strings.Join(append(j.parents, j.name), "/")
		if onlyFilter != "" && !strings.Contains(path, onlyFilter) {
			continue
		}
		fmt.Printf("Creating job: %s\n", path)

		var (
			job *gojenkins.Job
			err error
		)
		if len(j.parents) > 0 {
			job, err = jenkins.CreateJobInFolder(ctx, j.config, j.name, j.parents...)
		} else {
			job, err = jenkins.CreateJob(ctx, j.config, j.name)
		}
		if err != nil {
			fmt.Printf("  Could not create job: %v; checking for an existing job...\n", err)
			job, err = jenkins.GetJob(ctx, j.name, j.parents...)
			if err != nil {
				fmt.Printf("  Error fetching existing job: %v\n", err)
				continue
			}
			if err = job.UpdateConfig(ctx, j.config); err != nil {
				fmt.Printf("  Error updating existing job: %v\n", err)
				continue
			}
		}

		for i, params := range j.builds {
			if params == nil {
				params = map[string]string{}
			}
			fmt.Printf("  Triggering build #%d...\n", i+1)

			for attempt := range 60 {
				_ = attempt
				isQueued, qErr := job.IsQueued(ctx)
				if qErr != nil {
					fmt.Printf("  Error checking queue: %v\n", qErr)
					break
				}
				if !isQueued {
					break
				}
				fmt.Printf("  Waiting for queue to clear...\n")
				time.Sleep(3 * time.Second)
				job.Poll(ctx)
			}

			_, err := job.InvokeSimple(ctx, params)
			if err != nil {
				fmt.Printf("  Error triggering build: %v\n", err)
			}
		}
	}
}

func ansiColorScript() string {
	return `
pipeline {
    agent any
    options {
        ansiColor('xterm')
    }
    stages {
        stage('Levels') {
            steps {
                sh '''
                  printf '\033[1m=== ANSI color smoke test ===\033[0m\n'
                  printf '\033[31mERROR\033[0m failed to connect to upstream\n'
                  printf '\033[33mWARN\033[0m retrying in 2s\n'
                  printf '\033[32mINFO\033[0m connected\n'
                  printf '\033[36mDEBUG\033[0m payload=42\n'
                  printf '\033[1;34mINFO\033[0m bold blue heading\n'
                  printf '\033[41;37m FATAL \033[0m process crashed\n'
                  printf 'plain line without color\n'
                '''
            }
        }
        stage('Palette') {
            steps {
                sh '''
                  for c in 30 31 32 33 34 35 36 37; do
                    printf '\033[%smcolor %s\033[0m ' "$c" "$c"
                  done
                  printf '\n'
                  i=0
                  while [ "$i" -lt 16 ]; do
                    printf '\033[38;5;%sm█\033[0m' "$i"
                    i=$((i + 1))
                  done
                  printf '\n'
                '''
            }
        }
    }
}`
}

func logHeavyScript() string {
	// Generate a pipeline script that produces 1500+ lines of log output
	var sb strings.Builder
	sb.WriteString(`
pipeline {
    agent any
    stages {
        stage('Generate Logs') {
            steps {
                script {
`)
	// Generate echo statements that produce lots of log lines
	sb.WriteString("                    for (int i = 0; i < 500; i++) {\n")
	sb.WriteString("                        echo \"[INFO] Processing batch ${i}: Initializing data pipeline workers...\"\n")
	sb.WriteString("                        echo \"[DEBUG] Worker ${i} allocated 256MB heap, GC threshold at 80%\"\n")
	sb.WriteString("                        echo \"[INFO] Batch ${i} completed: 1024 records processed in ${i * 3}ms\"\n")
	sb.WriteString("                    }\n")
	sb.WriteString(`                }
            }
        }
    }
}`)
	return sb.String()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
