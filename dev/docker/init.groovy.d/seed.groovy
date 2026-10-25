import jenkins.model.*
import hudson.model.*
import org.jenkinsci.plugins.workflow.job.WorkflowJob
import org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition

def jenkins = Jenkins.instance

// ── Helper: create a pipeline job ────────────────────────────────────────────
def createPipelineJob(folder, String name, String pipelineScript) {
    def parent = folder ?: jenkins
    def job = parent.createProject(WorkflowJob, name)
    job.setDefinition(new CpsFlowDefinition(pipelineScript, true))
    job.save()
    println "[seed] Created job: ${folder ? folder.fullName + '/' : ''}${name}"
    return job
}

// ── Helper: create a folder ──────────────────────────────────────────────────
def createFolder(parent, String name) {
    def p = parent ?: jenkins
    def folder = p.createProject(com.cloudbees.hudson.plugins.folder.Folder, name)
    folder.save()
    println "[seed] Created folder: ${folder.fullName}"
    return folder
}

// ── Pipelines ────────────────────────────────────────────────────────────────
def simplePipeline = '''\
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Building...'
                sleep 2
            }
        }
        stage('Test') {
            steps {
                echo 'Running tests...'
                sleep 3
            }
        }
        stage('Deploy') {
            steps {
                echo 'Deploying...'
                sleep 1
            }
        }
    }
}
'''

def longPipeline = '''\
pipeline {
    agent any
    stages {
        stage('Checkout') {
            steps {
                echo 'Checking out source...'
                sleep 1
            }
        }
        stage('Compile') {
            steps {
                echo 'Compiling...'
                sleep 3
            }
        }
        stage('Unit Tests') {
            steps {
                echo 'Running unit tests...'
                sh 'for i in $(seq 1 50); do echo "Test $i passed"; sleep 0.1; done'
            }
        }
        stage('Integration Tests') {
            steps {
                echo 'Running integration tests...'
                sh 'for i in $(seq 1 30); do echo "Integration test $i passed"; sleep 0.2; done'
            }
        }
        stage('Package') {
            steps {
                echo 'Packaging artifact...'
                sleep 2
            }
        }
        stage('Deploy to Staging') {
            steps {
                echo 'Deploying to staging...'
                sleep 2
            }
        }
        stage('Smoke Tests') {
            steps {
                echo 'Running smoke tests...'
                sleep 1
            }
        }
    }
}
'''

def failingPipeline = '''\
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Building...'
                sleep 1
            }
        }
        stage('Test') {
            steps {
                echo 'This will fail'
                sh 'exit 1'
            }
        }
    }
}
'''

def parallelPipeline = '''\
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Building...'
                sleep 2
            }
        }
        stage('Tests') {
            parallel {
                stage('Unit') {
                    steps {
                        echo 'Unit tests...'
                        sleep 3
                    }
                }
                stage('Lint') {
                    steps {
                        echo 'Linting...'
                        sleep 2
                    }
                }
                stage('Security') {
                    steps {
                        echo 'Security scan...'
                        sleep 4
                    }
                }
            }
        }
        stage('Deploy') {
            steps {
                echo 'Deploying...'
                sleep 1
            }
        }
    }
}
'''

def parameterizedPipeline = '''\
pipeline {
    agent any
    parameters {
        string(name: 'VERSION', defaultValue: '1.0.0', description: 'Release version number')
        text(name: 'RELEASE_NOTES', defaultValue: 'Bug fixes and improvements', description: 'Release notes (multiline)')
        booleanParam(name: 'DRY_RUN', defaultValue: true, description: 'Run without deploying')
        choice(name: 'ENVIRONMENT', choices: ['dev', 'staging', 'production'], description: 'Target environment')
        password(name: 'DEPLOY_TOKEN', defaultValue: '', description: 'Deployment auth token')
    }
    stages {
        stage('Validate') {
            steps {
                echo "Version: ${params.VERSION}"
                echo "Environment: ${params.ENVIRONMENT}"
                echo "Dry run: ${params.DRY_RUN}"
                echo "Release notes: ${params.RELEASE_NOTES}"
                sleep 1
            }
        }
        stage('Build') {
            steps {
                echo "Building version ${params.VERSION}..."
                sleep 2
            }
        }
        stage('Deploy') {
            when { expression { !params.DRY_RUN } }
            steps {
                echo "Deploying to ${params.ENVIRONMENT}..."
                sleep 2
            }
        }
    }
}
'''

// ── Create top-level jobs ────────────────────────────────────────────────────
createPipelineJob(null, "simple-build", simplePipeline)
createPipelineJob(null, "full-pipeline", longPipeline)
createPipelineJob(null, "failing-job", failingPipeline)
createPipelineJob(null, "parallel-tests", parallelPipeline)
createPipelineJob(null, "parameterized-deploy", parameterizedPipeline)

// ── Create folders with nested jobs ──────────────────────────────────────────
def backendFolder = createFolder(null, "backend")
createPipelineJob(backendFolder, "api-service", simplePipeline)
createPipelineJob(backendFolder, "auth-service", longPipeline)
createPipelineJob(backendFolder, "worker", failingPipeline)

def frontendFolder = createFolder(null, "frontend")
createPipelineJob(frontendFolder, "web-app", parallelPipeline)
createPipelineJob(frontendFolder, "mobile-app", simplePipeline)

def infraFolder = createFolder(null, "infra")
createPipelineJob(infraFolder, "terraform-plan", simplePipeline)
createPipelineJob(infraFolder, "deploy-prod", longPipeline)

// Nested folder
def sharedLibs = createFolder(infraFolder, "shared-libs")
createPipelineJob(sharedLibs, "common-utils", simplePipeline)
createPipelineJob(sharedLibs, "ci-templates", simplePipeline)

jenkins.save()
println "[seed] Seed complete!"
