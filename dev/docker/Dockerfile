FROM jenkins/jenkins:lts

# Switch to jenkins user (already exists in the base image)
USER jenkins

# Add a plugin list and preinstall with the official CLI
COPY plugins.txt /usr/share/jenkins/ref/plugins.txt
RUN jenkins-plugin-cli --latest false --plugin-file /usr/share/jenkins/ref/plugins.txt

# Seed: create user + sample jobs on first boot
COPY init.groovy.d/ /usr/share/jenkins/ref/init.groovy.d/
