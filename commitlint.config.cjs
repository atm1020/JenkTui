module.exports = {
  rules: {
    "type-case": [2, "always", "lower-case"],
    "type-empty": [2, "never"],
    "type-enum": [
      2,
      "always",
      [
        "build",
        "chore",
        "ci",
        "docs",
        "feat",
        "fix",
        "perf",
        "refactor",
        "revert",
        "style",
        "test"
      ]
    ],
    "scope-case": [2, "always", "kebab-case"],
    "subject-case": [0],
    "subject-empty": [2, "never"]
  }
}
