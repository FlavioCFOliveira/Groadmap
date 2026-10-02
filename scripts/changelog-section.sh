#!/usr/bin/env bash
# changelog-section.sh prints the body of one version's section of CHANGELOG.md.
#
# Usage: scripts/changelog-section.sh <version> [changelog-path]
#
# <version> is X.Y.Z, with or without one leading "v" (a release tag such as
# v1.18.0 is accepted as it is). [changelog-path] defaults to CHANGELOG.md in
# the current directory.
#
# The rule is SPEC/DEPLOY.md § GitHub Release Body. The section is the one whose
# heading line is exactly "## [X.Y.Z]" or "## [X.Y.Z] - YYYY-MM-DD". Its body is
# every line after the heading, up to but not including the next line that
# starts with "## [", or up to the end of the file. Leading and trailing blank
# lines are removed; the lines between them are printed exactly as written.
#
# The version is compared as a literal string, never as a pattern, so 1.1.0
# selects neither "## [1.1.0-rc1]" nor "## [11.1.0]".
#
# Exit status: 0 with the body on standard output; 1 when the file holds no
# heading for the version, more than one heading for it, or a section whose
# body is empty once trimmed; 2 on a usage error or an unreadable file. Every
# failure writes one message naming the version to standard error and nothing
# to standard output.
set -euo pipefail

prog="changelog-section.sh"

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: ${prog} <version> [changelog-path]" >&2
  exit 2
fi

version="${1#v}"
changelog="${2:-CHANGELOG.md}"

if [ -z "${version}" ]; then
  echo "${prog}: the version is empty; expected X.Y.Z, with or without a leading v" >&2
  exit 2
fi

if [ ! -f "${changelog}" ] || [ ! -r "${changelog}" ]; then
  echo "${prog}: cannot read ${changelog} to extract the section for version ${version}" >&2
  exit 2
fi

# The awk program reads the version from the environment rather than from
# -v, which would interpret backslash escapes in it. It prints the trimmed
# body, or exits 3 (no heading), 4 (duplicate headings) or 5 (empty body),
# writing the line numbers of the matching headings for the duplicate case.
status=0
body="$(
  CHANGELOG_VERSION="${version}" LC_ALL=C awk '
    function is_heading(line,    prefix, rest) {
      prefix = "## [" ver "]"
      if (line == prefix) return 1
      if (substr(line, 1, length(prefix)) != prefix) return 0
      rest = substr(line, length(prefix) + 1)
      return rest ~ /^ - [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/
    }
    BEGIN { ver = ENVIRON["CHANGELOG_VERSION"]; headings = 0; in_section = 0; n = 0 }
    {
      if (is_heading($0)) {
        headings++
        where = (where == "" ? NR : where ", " NR)
        in_section = (headings == 1)
        next
      }
      if (substr($0, 1, 4) == "## [") { in_section = 0; next }
      if (in_section) lines[++n] = $0
    }
    END {
      if (headings == 0) exit 3
      if (headings > 1) { print where; exit 4 }
      first = 1
      while (first <= n && lines[first] ~ /^[ \t]*$/) first++
      last = n
      while (last >= first && lines[last] ~ /^[ \t]*$/) last--
      if (first > last) exit 5
      for (i = first; i <= last; i++) print lines[i]
    }
  ' "${changelog}"
)" || status=$?

case "${status}" in
  0)
    printf '%s\n' "${body}"
    ;;
  3)
    echo "${prog}: ${changelog} holds no section for version ${version}: no heading '## [${version}]' or '## [${version}] - YYYY-MM-DD' was found" >&2
    exit 1
    ;;
  4)
    echo "${prog}: ${changelog} holds duplicate sections for version ${version} (headings at lines ${body}); merge them into one section" >&2
    exit 1
    ;;
  5)
    echo "${prog}: the section for version ${version} in ${changelog} is empty: its heading is followed by no content" >&2
    exit 1
    ;;
  *)
    echo "${prog}: extracting the section for version ${version} from ${changelog} failed (awk exit status ${status})" >&2
    exit 2
    ;;
esac
