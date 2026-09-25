---
name: write-changelog
description: Write or update a VPSBox release changelog entry and platform-specific social posts from git history and relevant worktree changes. Use when preparing a VPSBox desktop release, drafting release notes, documenting changes since a tag, or creating Discord, Reddit, LinkedIn, X, and Vietnamese Facebook launch content.
---

# Write VPSBox Changelog

Prepare accurate, user-facing release content for VPSBox without tagging, committing, publishing, or changing application versions.

## Input

Interpret the invocation as:

```text
$write-changelog [version] [optional emphasis or exclusions]
```

Accept `1.2.3` or `v1.2.3` and normalize it to `1.2.3`. If no version is supplied, increment the patch component of the latest semantic version tag; for example, `v1.0.1` becomes `1.0.2`. If the repository has no version tag, default to `0.1.0` and report that assumption.

Treat the remaining text as editorial direction, not as evidence that a feature exists. Verify every release claim against the repository.

## Gather the Release Scope

Work from the repository root and preserve all unrelated user changes.

1. Read `CHANGELOG.md` before editing it. If it does not exist, plan to create it with `# Changelog` followed by the new entry.
2. Read the newest `social-content/content-*.md` file if one exists. Match its useful conventions without copying obsolete product claims.
3. Inspect tags and status:

   ```bash
   git tag --sort=-version:refname
   git status --short
   ```

4. Resolve the comparison range:
   - If `v<version>` already exists, treat this as historical release documentation. Compare the preceding semantic version tag with `v<version>` and do not mix later or uncommitted changes into the entry.
   - Otherwise, compare the latest semantic version tag reachable from `HEAD` with `HEAD`.
   - If no baseline tag exists, inspect all commits reachable from `HEAD`.
5. Inspect both the summary and the substance of the range:

   ```bash
   git log --format='%h %s%n%b' <baseline>..HEAD
   git diff --stat <baseline>..HEAD
   git diff <baseline>..HEAD
   ```

   Adjust `HEAD` to the target tag for historical releases.
6. For an upcoming untagged release, also inspect staged, unstaged, and relevant untracked files. Include worktree behavior only when it is clearly intended for this release or the user's notes request it. Distinguish it from committed history in the final summary.
7. Read the implementation, tests, UI copy, documentation, and workflow changes needed to understand user-visible behavior. Do not rely only on commit subjects.
8. Build a private inventory of candidate changes. Exclude refactors, generated files, dependency churn, tests, and internal documentation unless they materially change the user experience.

If the target version already has an entry in `CHANGELOG.md`, stop and ask before replacing or merging it. An existing git tag alone is not a blocker because the user may be documenting a historical release.

## Classify Changes

Use only non-empty categories:

| Category | Use for |
| --- | --- |
| `New Features` | New actions or workflows users could not perform before |
| `Improvements` | Better usability, reliability, installation, performance, or platform support |
| `Bug Fixes` | Incorrect or failing behavior that now works |

Mention affected platforms when relevant: macOS, Windows, or Linux. Treat Docker, SSH, TLS, Multipass, Hyper-V, VPS, VM, and self-hosting as user-facing terms in this product; explain less familiar terms through their benefit.

## Write `CHANGELOG.md`

Insert the release immediately after the top-level `# Changelog` heading. Use:

```markdown
# [X.Y.Z] - Short Release Title

## New Features

- **Action-oriented key phrase** — Describe what users can now do and why it helps.

## Improvements

- **Specific improvement** — Describe the visible benefit and name the platform when needed.

## Bug Fixes

- Fixed **the specific behavior users experienced** — Describe what works now.
```

Apply these rules:

- Create a specific, memorable title of roughly two to six words that reflects the main release theme.
- Describe outcomes from the user's perspective. State what changed, where it applies, and why it matters.
- Bold the key phrase in every bullet. Use sub-bullets only for genuinely useful detail.
- Use action verbs such as Create, Start, Connect, Export, Restore, Share, Install, and Manage.
- Prefer concrete language such as “Fixed Multipass installation being reported as failed when it was already installed” over “Fixed installer issue.”
- Avoid file paths, symbols, schema details, internal package names, code-level explanations, and unsupported marketing claims.
- Do not present build tooling, tests, refactors, or CI-only maintenance as product features. Signing and packaging changes may be included when they directly improve installation trust or availability.
- Keep the entry concise while covering every meaningful user-visible change in the release scope.

## Write Social Content

Create `social-content/content-<version>.md`, creating the directory when needed. Use the changelog as the factual source and include these sections in order:

1. `Discord` — Put the complete post inside a fenced code block. Include a release heading, one-sentence summary, emoji category headings, feature bullets, and a closing CTA.
2. `Reddit` — Add a `**Title**:` line, open conversationally, explain the main benefits in prose, invite feedback, and avoid corporate phrasing.
3. `LinkedIn` — Use a professional opener, a `Key highlights:` list, a download CTA, and relevant hashtags.
4. `X (Twitter)` — Keep the entire post at or below 280 characters, including whitespace and the URL. Highlight only the strongest two to four changes.
5. `Facebook (Vietnamese)` — Write a natural Vietnamese adaptation of the Discord post, preserving all important facts without translating word for word.

Start the file with:

```markdown
# Social Media Content for vX.Y.Z
```

Separate platform sections with `---`. Use emojis sparingly for scannable headings. Keep product capitalization as `VPSBox` in prose and use familiar Vietnamese developer terms such as “triển khai,” “cài đặt,” “máy ảo,” and “quản lý.”

Use `https://github.com/stoicsoft/vpsbox-app/releases` as the download or release CTA unless the repository establishes a newer canonical VPSBox landing page. Use platform-appropriate tags such as `#VPSBox #DevOps #SelfHosted #Docker #Virtualization`; use `#QuanLyServer` where natural in Vietnamese.

## Validate

Before reporting completion:

1. Reconcile the final bullets against the private change inventory. Remove claims that cannot be supported and add omitted user-visible changes.
2. Confirm the changelog contains the target version exactly once and that empty categories are absent.
3. Count the X post characters and reduce it to 280 or fewer.
4. Check Markdown structure, fenced code blocks, version consistency, links, and natural Vietnamese wording.
5. Run:

   ```bash
   git diff --check
   git diff -- CHANGELOG.md social-content/content-<version>.md
   ```

Do not run application tests for content-only changes unless the user requests them. Do not alter version files, create a tag, commit, push, publish a release, or post to social platforms.

## Report

Lead with the completed version and list:

- Comparison range and whether uncommitted release work was included
- Number of commits reviewed
- Counts of features, improvements, and fixes written
- Files created or modified
- Any uncertainty or change intentionally omitted

If the scope contains only one or two minor fixes, still complete an explicitly requested changelog, then note that the release is small and worth reviewing before publication.
