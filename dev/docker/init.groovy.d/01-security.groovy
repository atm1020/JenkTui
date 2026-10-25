import hudson.security.FullControlOnceLoggedInAuthorizationStrategy
import hudson.security.HudsonPrivateSecurityRealm
import hudson.model.User
import jenkins.model.Jenkins

def jenkins = Jenkins.instance

def adminUser = System.getenv("JENKINS_ADMIN_ID") ?: "admin"
def adminPassword = System.getenv("JENKINS_ADMIN_PASSWORD") ?: "admin"
def extraUsers = System.getenv("JENKINS_USERS") ?: ""

def realm = new HudsonPrivateSecurityRealm(false)

def ensureUser = { String username, String password ->
    if (User.getById(username, false) != null) {
        println "[init-security] User already exists, skipping create: ${username}"
        return
    }

    realm.createAccount(username, password)
    println "[init-security] Created user: ${username}"
}

ensureUser(adminUser, adminPassword)

extraUsers.split(",")
    .collect { it.trim() }
    .findAll { !it.isEmpty() }
    .each { token ->
        def parts = token.split(":", 2)
        if (parts.length != 2 || parts[0].trim().isEmpty() || parts[1].trim().isEmpty()) {
            println "[init-security] Skipping invalid JENKINS_USERS entry: ${token}"
            return
        }

        def username = parts[0].trim()
        def password = parts[1].trim()
        ensureUser(username, password)
    }

jenkins.setSecurityRealm(realm)

def strategy = new FullControlOnceLoggedInAuthorizationStrategy()
strategy.setAllowAnonymousRead(false)
jenkins.setAuthorizationStrategy(strategy)

jenkins.save()
println "[init-security] Security realm and auth strategy configured"
