# security-hub

> Aggregated [OpenSSF Scorecard](https://github.com/ossf/scorecard) reporting across an entire self-hosted GitLab instance.

**Status:** early development — architecture below reflects the target design; see [Roadmap](#roadmap).

## What it does

`security-hub` walks every group, subgroup, and project on a self-hosted GitLab instance, runs an [OpenSSF Scorecard](https://github.com/ossf/scorecard) analysis against each project, and rolls the results up into a single browsable HTML report. Instead of looking at one repo's score in isolation, you get:

- An **instance-level overview**: average score per Scorecard check, plus one overall average, across every project GitLab knows about.
- **Per-group and per-subgroup rollups**, computed the same way, so you can see which part of the org is lagging without opening every project.
- **Per-project detail**, down to the individual Scorecard check results, at the leaf of the tree.

Aggregation is recursive: a group's numbers are the average of its direct projects and its subgroups' (already-aggregated) numbers, all the way up to the instance root.

```
Instance (avg per check + overall)
└─ Group (avg per check + overall)
   ├─ Subgroup (avg per check + overall)
   │  └─ Project (raw Scorecard result)
   └─ Project (raw Scorecard result)
```

## How it works

1. **Discover** — list every project visible to the configured token via the GitLab API. Groups and subgroups are never fetched: they're inferred directly from each project's namespaced path (`team/backend/service` implies groups `team` and `team/backend`).
2. **Analyze** — run Scorecard against each project's repository URL, collecting a score per check (`Branch-Protection` `Code-Review`, `Dangerous-Workflow`, `Vulnerabilities`, etc.) plus Scorecard's own aggregate score. Scorecard is used as a Go library ([`github.com/ossf/scorecard`](https://github.com/ossf/scorecard)), not shelled out to.
3. **Aggregate** — average every check bottom-up through the group tree (project → subgroup → group → instance).
4. **Render** — emit a single self-contained HTML report with a drill-down view: instance summary first, then group → subgroup → project navigation, each level showing its own aggregated scores.

### Offline mode

By default, `security-hub` only ever talks to the internal GitLab instance — all analysis is done against a local clone of each repository. A handful of Scorecard checks (`Vulnerabilities` via OSV.dev, `CII-Best-Practices` via bestpractices.dev, `Fuzzing`'s OSS-Fuzz status) additionally call out to public internet services regardless of where the repo is hosted. These run by default.

Set `scorecard.offline: true` (or `SECURITY_HUB_OFFLINE=true`) to disable that subset and restrict the run to checks that only need the local clone and the internal GitLab API — for fully airgapped environments with zero internet egress. Disabled checks are shown in the report as `N/A` rather than silently omitted.

## Supported checks

Scorecard ships more checks than GitLab can actually support: several rely on GitHub- or Azure DevOps-specific APIs and artifacts (release assets, webhooks, Actions workflow permissions) that have no GitLab equivalent. When `scorecard.checks` is left empty, security-hub requests every check below — the ones marked ❌ are silently dropped by Scorecard itself before they run (Scorecard's own check registry doesn't declare GitLab support for them), so they never appear in the report at all, not even as `N/A`. This is a structural Scorecard-for-GitLab limitation, not a security-hub bug.

| Check | Runs on GitLab? | Notes |
|---|---|---|
| Binary-Artifacts | ✅ | |
| Branch-Protection | ✅ | GitLab associates releases with commits rather than branches; that part of the scoring is skipped |
| CI-Tests | ✅ | |
| CII-Best-Practices | ✅ | Requires internet access (bestpractices.dev) — shown as `N/A` when `scorecard.offline: true` |
| Code-Review | ✅ | |
| Dependency-Update-Tool | ✅ | |
| Fuzzing | ✅ | Requires internet access (OSS-Fuzz) — shown as `N/A` when `scorecard.offline: true` |
| License | ✅ | |
| Maintained | ✅ | |
| Pinned-Dependencies | ✅ | |
| Security-Policy | ✅ | |
| Vulnerabilities | ✅ | Requires internet access (OSV.dev) — shown as `N/A` when `scorecard.offline: true` |
| Contributors | ❌ | GitHub- and Azure DevOps-only |
| Dangerous-Workflow | ❌ | Analyzes GitHub Actions workflow syntax |
| Packaging | ❌ | Looks for GitHub Packages publish workflows |
| SAST | ❌ | Looks for CodeQL/SonarCloud GitHub apps |
| Signed-Releases | ❌ | Looks for GitHub release assets |
| Token-Permissions | ❌ | Analyzes GitHub Actions workflow token permissions |
| SBOM, Webhooks | ❌ | Excluded by Scorecard itself unless its experimental flag is set, which security-hub doesn't set |

## Requirements

- Go (see `go.mod` once initialized)
- Network access to the target GitLab instance's API
- A GitLab token with at least read access to the groups/projects you want
  scanned (`read_api` + `read_repository`)

## Configuration

Both a config file and environment variables are supported; environment variables override the config file.

```yaml
# config.yaml
gitlab:
  url: https://gitlab.example.com
  token: ${GITLAB_TOKEN}
scorecard:
  checks: []            # empty = all checks
  offline: false        # true = disable checks requiring internet access
  maxConcurrency: 5     # empty/0/negative = no concurrency limit
output:
  path: ./report
```

| Env var                | Purpose                                                        |
|-------------------------|------------------------------------------------------------------|
| `GITLAB_URL`            | Base URL of the self-hosted GitLab instance                     |
| `GITLAB_TOKEN`          | API token used for discovery + repo access                       |
| `SECURITY_HUB_OFFLINE`  | `true` to disable Scorecard checks that require internet access |

`scorecard.maxConcurrency` bounds how many projects are scanned with Scorecard at once, so a large instance doesn't overwhelm the GitLab API or the local machine. Leave it unset (or set it to `0` or a negative number) to run every project's scan concurrently with no limit.

## Usage

```bash
security-hub scan --config config.yaml
```

This produces a report directory (default `./report`) containing the aggregated HTML output plus its `static/css` and `static/js` assets, openable directly in a browser with no server required. From the report, **Export CSV** downloads the full tree (every group/subgroup/project, with per-check scores) as a spreadsheet-ready CSV, and **Export PDF** opens the browser's print dialog with a PDF-friendly layout of the currently viewed page.

The report's styling is built with [Tailwind CSS](https://tailwindcss.com/) from `internal/report/tailwind/input.css`. The compiled `internal/report/static/css/app.css` is committed, so a plain `go build` never needs Tailwind; only run `make frontend` if you edit the report's styles (it downloads the standalone Tailwind CLI into `./bin` on first use — no Node/npm required).

Logging is structured (via [zap](https://github.com/uber-go/zap)) and written to stderr at `info` level by default. Pass `-v`/`--verbose` (works on any subcommand) to switch to `debug` level, which also surfaces per-project scan detail and otherwise-hidden diagnostic output from dependencies (e.g. Scorecard's GitLab tarball fetch attempts):

```bash
security-hub -v scan --config config.yaml
```

## Roadmap

- [ ] GitLab project discovery (paginated; groups inferred from project paths)
- [ ] Scorecard integration as a Go library
- [ ] Bottom-up aggregation across the group tree
- [ ] Static HTML report with drill-down navigation
- [ ] Config file + env var configuration
- [ ] CI-friendly mode (exit non-zero below a score threshold)
