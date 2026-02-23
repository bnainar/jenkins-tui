import jenkins.model.*
import hudson.model.*
import org.jenkinsci.plugins.workflow.job.WorkflowJob
import org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition

Jenkins j = Jenkins.instance

def existing = j.getItem("perm-test")
if (existing == null) {
  WorkflowJob job = j.createProject(WorkflowJob, "perm-test")
  def script = '''
properties([
  parameters([
    choice(name: 'REGION', choices: ['US-VIRGINIA', 'EU-FRANKFURT', 'AP-MUMBAI'], description: 'Region'),
    choice(name: 'ACTION', choices: ['drain', 'reload'], description: 'Worker action'),
    string(name: 'REASON', defaultValue: 'maintenance', description: 'Reason'),
    booleanParam(name: 'DRY_RUN', defaultValue: true, description: 'Dry run mode')
  ])
])

pipeline {
  agent any
  stages {
    stage('Echo') {
      steps {
        echo "REGION=${params.REGION} ACTION=${params.ACTION} REASON=${params.REASON} DRY_RUN=${params.DRY_RUN}"
      }
    }
    stage('Sleep') {
      steps {
        sleep(time: 5, unit: 'SECONDS')
      }
    }
  }
}
'''
  job.setDefinition(new CpsFlowDefinition(script, true))
  job.save()
  println "Created seeded job: perm-test"
}
