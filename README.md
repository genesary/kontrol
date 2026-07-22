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

1. **Discover** — recursively list groups, subgroups, and projects from the GitLab API, starting at the instance (or a configured root group).
2. **Analyze** — run Scorecard against each project's repository URL, collecting a score per check (`Branch-Protection` `Code-Review`, `Dangerous-Workflow`, `Vulnerabilities`, etc.) plus Scorecard's own aggregate score. Scorecard is used as a Go library ([`github.com/ossf/scorecard`](https://github.com/ossf/scorecard)), not shelled out to.
3. **Aggregate** — average every check bottom-up through the group tree (project → subgroup → group → instance).
4. **Render** — emit a single self-contained HTML report with a drill-down view: instance summary first, then group → subgroup → project navigation, each level showing its own aggregated scores.

### Offline mode

By default, `security-hub` only ever talks to the internal GitLab instance — all analysis is done against a local clone of each repository. A handful of Scorecard checks (`Vulnerabilities` via OSV.dev, `CII-Best-Practices` via bestpractices.dev, `Fuzzing`'s OSS-Fuzz status) additionally call out to public internet services regardless of where the repo is hosted. These run by default.

Set `scorecard.offline: true` (or `SECURITY_HUB_OFFLINE=true`) to disable that subset and restrict the run to checks that only need the local clone and the internal GitLab API — for fully airgapped environments with zero internet egress. Disabled checks are shown in the report as "unavailable (offline mode)" rather than silently omitted.

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
  root_group: ""       # empty = entire instance
scorecard:
  checks: []            # empty = all checks
  offline: false        # true = disable checks requiring internet access
output:
  path: ./report
```

| Env var                | Purpose                                                        |
|-------------------------|------------------------------------------------------------------|
| `GITLAB_URL`            | Base URL of the self-hosted GitLab instance                     |
| `GITLAB_TOKEN`          | API token used for discovery + repo access                       |
| `SECURITY_HUB_OFFLINE`  | `true` to disable Scorecard checks that require internet access |

## Usage

```bash
security-hub scan --config config.yaml
```

This produces a report directory (default `./report`) containing the aggregated HTML output, openable directly in a browser with no server required.

## Roadmap

- [ ] GitLab group/project discovery (recursive, paginated)
- [ ] Scorecard integration as a Go library
- [ ] Bottom-up aggregation across the group tree
- [ ] Static HTML report with drill-down navigation
- [ ] Config file + env var configuration
- [ ] CI-friendly mode (exit non-zero below a score threshold)
