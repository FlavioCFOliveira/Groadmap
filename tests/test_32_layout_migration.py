#!/usr/bin/env python3
"""
Test 32: Filesystem layout migration (legacy -> current).

Exercises the automatic startup sweep specified in
SPEC/ARCHITECTURE.md § Filesystem Layout Migration. The sweep relocates
roadmaps from the LEGACY layout (~/.roadmaps/<name>.db) to the CURRENT
per-roadmap layout (~/.roadmaps/<name>/project.db) on every `rmp`
invocation, before command routing.

Because the CLI only ever creates the current layout, a genuine legacy
state is fabricated on the filesystem the same way the adversarial QA
pass did it: create a roadmap via the CLI (which produces
~/.roadmaps/<name>/project.db), populate it with real tasks and sprints,
then MOVE project.db (plus any -wal/-shm sidecars) back out to the
top-level ~/.roadmaps/<name>.db and delete the now-empty <name>/ dir.
Running any real command then triggers the sweep.

Coverage:
  (a) Data is fully preserved: every task and sprint created before the
      fabrication is intact and queryable after the sweep.
  (b) The migrated database lives at ~/.roadmaps/<name>/project.db with
      mode 0600, inside a ~/.roadmaps/<name>/ directory with mode 0700.
  (c) The legacy top-level ~/.roadmaps/<name>.db file is gone.
  (d) A second run is a clean idempotent no-op (no errors, layout
      unchanged).
  (e) Security regression guard: a top-level symlink whose name ends in
      .db and which points OUTSIDE the data directory is left untouched
      and its target is never modified (no follow, no chmod, no rename).
  (f) `roadmap list` reports the path as ~/.roadmaps/<name>/project.db.
  (g) Conflict/data-safety: when a real ~/.roadmaps/<name>/project.db
      already exists alongside a legacy ~/.roadmaps/<name>.db, the
      conflict (keyed on project.db) is non-fatal, project.db is not
      overwritten by the atomic rename, and the legacy file is untouched.
"""

import os
import stat
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# Sidecar suffixes the migration relocates alongside the database, mirroring
# internal/utils/migrate.go. They are moved only when present.
SIDECAR_SUFFIXES = ("-wal", "-shm")


class TestLayoutMigration:
    """Drive a fabricated legacy layout through the automatic startup sweep."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        # A realistic, fixed roadmap name (not a random uuid) so the on-disk
        # path assertions read clearly.
        self.roadmap = "inventory-platform"
        self.test.create_roadmap(self.roadmap)

        # Populate with realistic tasks and a sprint so we can prove data
        # preservation across the move.
        self.task_titles = [
            "Wire reorder webhook into the supplier gateway",
            "Add idempotency keys to stock-adjustment writes",
            "Backfill warehouse location codes for legacy SKUs",
        ]
        self.task_ids = []
        for title in self.task_titles:
            tid = self.test.create_task(
                self.roadmap,
                title=title,
                functional_requirements="Required for the inventory reliability initiative.",
                technical_requirements="Implement behind a feature flag; cover with integration tests.",
                acceptance_criteria="No regressions in the nightly stock reconciliation job.",
                priority=6,
            )
            self.task_ids.append(tid)
        self.sprint_id = self.test.create_sprint(
            self.roadmap, "Q3 inventory reliability sprint"
        )

        self.roadmaps_dir = self.test.roadmaps_dir
        self.home_dir = self.roadmaps_dir / self.roadmap
        self.current_db = self.home_dir / "project.db"
        self.legacy_db = self.roadmaps_dir / f"{self.roadmap}.db"

    def teardown_method(self):
        self.test.teardown()

    # --- helpers --------------------------------------------------------

    def _fabricate_legacy_layout(self):
        """Move ~/.roadmaps/<name>/project.db (+sidecars) out to
        ~/.roadmaps/<name>.db and remove the now-empty <name>/ dir, so the
        on-disk state is the legacy layout the sweep is meant to migrate."""
        assert self.current_db.exists(), (
            "precondition: CLI must have created the current-layout database"
        )
        # Move the database itself.
        self.current_db.rename(self.legacy_db)
        # Move any sidecars that happen to be present.
        for suffix in SIDECAR_SUFFIXES:
            src = self.home_dir / f"project.db{suffix}"
            if src.exists():
                src.rename(self.roadmaps_dir / f"{self.roadmap}.db{suffix}")
        # The home directory must now be empty; remove it to complete the
        # legacy fabrication.
        leftover = list(self.home_dir.iterdir())
        assert not leftover, f"home dir should be empty before rmdir, found {leftover}"
        self.home_dir.rmdir()

        # Sanity: we are genuinely in the legacy layout now.
        assert self.legacy_db.exists(), "fabrication failed: legacy db missing"
        assert not self.home_dir.exists(), "fabrication failed: home dir still present"

    def _mode(self, path):
        return stat.S_IMODE(os.lstat(path).st_mode)

    def _trigger_sweep(self):
        """Run a real command so the startup sweep fires. `roadmap list` is a
        read-only command that still runs the sweep before routing."""
        return self.test.run_cmd_json(["roadmap", "list"])

    # --- tests ----------------------------------------------------------

    def test_sweep_migrates_legacy_layout_preserving_data(self):
        """(a)(b)(c): a fabricated legacy db is migrated in place, data intact,
        permissions correct, legacy file gone."""
        self._fabricate_legacy_layout()

        # Trigger the sweep by running a real command.
        self._trigger_sweep()

        # (c) Legacy top-level file is gone.
        assert not self.legacy_db.exists(), (
            f"legacy top-level db must be removed by the sweep: {self.legacy_db}"
        )
        # (b) Current layout exists with the correct permissions.
        assert self.current_db.exists(), (
            f"migrated database must exist at {self.current_db}"
        )
        assert self._mode(self.home_dir) == 0o700, (
            f"roadmap home dir mode must be 0700, got {oct(self._mode(self.home_dir))}"
        )
        assert self._mode(self.current_db) == 0o600, (
            f"migrated db mode must be 0600, got {oct(self._mode(self.current_db))}"
        )

        # (a) Data fully preserved: every task and the sprint are queryable
        # at the new path, with identical content.
        listed = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        listed_titles = {t["title"] for t in listed}
        for title in self.task_titles:
            assert title in listed_titles, (
                f"task {title!r} lost across migration; got {sorted(listed_titles)}"
            )
        for tid in self.task_ids:
            task = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(tid)])
            obj = task[0] if isinstance(task, list) else task
            assert obj["id"] == tid, f"task {tid} not retrievable post-migration"

        sprint = self.test.run_cmd_json(
            ["sprint", "get", "-r", self.roadmap, str(self.sprint_id)]
        )
        assert sprint["description"] == "Q3 inventory reliability sprint", (
            f"sprint data not preserved across migration: {sprint!r}"
        )
        print("✓ legacy layout migrated in place; tasks/sprint preserved; perms 0700/0600; legacy file removed")

    def test_roadmap_list_reports_current_path(self):
        """(f): after migration, `roadmap list` reports the current-layout path."""
        self._fabricate_legacy_layout()

        listing = self._trigger_sweep()
        entry = next((r for r in listing if r["name"] == self.roadmap), None)
        assert entry is not None, f"roadmap {self.roadmap!r} missing from list: {listing!r}"
        expected = str(self.current_db)
        assert entry["path"] == expected, (
            f"roadmap list path must be the current layout; got {entry['path']!r}, want {expected!r}"
        )
        print(f"✓ roadmap list reports the migrated path {entry['path']}")

    def test_second_sweep_is_idempotent_noop(self):
        """(d): a second invocation after migration changes nothing."""
        self._fabricate_legacy_layout()

        # First sweep migrates.
        self._trigger_sweep()
        first_db_bytes = self.current_db.read_bytes()
        first_mode = self._mode(self.current_db)
        first_dir_mode = self._mode(self.home_dir)

        # Second sweep: no legacy file remains, so it must be a clean no-op.
        exit_code, _, stderr = self.test.run_cmd(["roadmap", "list"], check=False)
        assert exit_code == 0, f"idempotent re-run must succeed, got exit {exit_code}, stderr={stderr!r}"

        assert not self.legacy_db.exists(), "no legacy file should reappear"
        assert self.current_db.exists(), "migrated db must remain in place"
        assert self.current_db.read_bytes() == first_db_bytes, (
            "idempotent re-run must not alter the migrated database"
        )
        assert self._mode(self.current_db) == first_mode == 0o600
        assert self._mode(self.home_dir) == first_dir_mode == 0o700
        print("✓ second sweep is a clean idempotent no-op")

    def test_top_level_symlink_outside_datadir_is_untouched(self):
        """(e) SECURITY: a top-level .db symlink pointing OUTSIDE the data
        directory must be left completely untouched, its target unmodified,
        and no roadmap home directory created for it. This guards against the
        symlink-follow vulnerability where the sweep would rename the link and
        chmod its external target."""
        # An external regular file with known permissions, OUTSIDE ~/.roadmaps.
        external_dir = self.test.home_dir / "external_secrets"
        external_dir.mkdir()
        external_file = external_dir / "production-credentials.txt"
        external_file.write_text("EXTERNAL-PAYLOAD-DO-NOT-TOUCH")
        os.chmod(external_file, 0o644)
        original_mode = self._mode(external_file)
        original_bytes = external_file.read_bytes()

        # A top-level .db entry that is a SYMLINK to the external file.
        evil_link = self.roadmaps_dir / "evil.db"
        os.symlink(str(external_file), str(evil_link))

        # Migrate the real roadmap in the same pass so the sweep is genuinely
        # iterating top-level entries (the link must be skipped, the real
        # roadmap migrated).
        self._fabricate_legacy_layout()

        exit_code, _, _ = self.test.run_cmd(["roadmap", "list"], check=False)
        assert exit_code == 0, f"sweep must not be fatal on a symlink entry, exit {exit_code}"

        # The symlink itself is untouched: still a symlink, still pointing at
        # the same external target.
        assert evil_link.is_symlink(), "evil.db must still be a symlink after the sweep"
        assert os.readlink(str(evil_link)) == str(external_file), (
            "symlink target must be unchanged"
        )
        # The external target is untouched: same permissions, same content,
        # same location (chmod must NOT have followed the link).
        assert self._mode(external_file) == original_mode == 0o644, (
            f"external target perms must be unchanged (0644), got {oct(self._mode(external_file))}"
        )
        assert external_file.read_bytes() == original_bytes, (
            "external target content must be unchanged"
        )
        # No roadmap home directory was fabricated from the link.
        assert not (self.roadmaps_dir / "evil").exists(), (
            "no home directory must be created for a symlink entry"
        )
        # The legitimate roadmap still migrated correctly alongside the link.
        assert self.current_db.exists(), "the real roadmap must still migrate"
        assert not self.legacy_db.exists(), "the real legacy db must still be removed"
        print("✓ top-level symlink pointing outside the data dir left untouched; target unmodified; real roadmap still migrated")

    def test_conflict_existing_current_db_not_overwritten(self):
        """(g) CONFLICT/DATA-SAFETY: when BOTH the legacy ~/.roadmaps/<name>.db
        and a real current ~/.roadmaps/<name>/project.db exist, the conflict is
        keyed on project.db. The current layout wins: the sweep is non-fatal,
        project.db is NOT overwritten by the legacy file (the atomic rename is
        never reached), and the legacy file is left untouched.

        The conflict is constructed via a real <name>/project.db (not merely an
        empty directory), because the refined rule keys the conflict on the
        database FILE."""
        # Start from the genuine current layout the CLI produced, then ALSO
        # fabricate a legacy top-level file with DISTINCT content. The legacy
        # file is a verbatim copy with a marker prepended so its bytes differ
        # from the live project.db; project.db itself is left in place.
        live_db_bytes = self.current_db.read_bytes()
        legacy_marker = b"LEGACY-CONFLICT-MARKER-MUST-NOT-WIN\n"
        self.legacy_db.write_bytes(legacy_marker + live_db_bytes)

        assert self.current_db.exists(), "precondition: current project.db must exist"
        assert self.legacy_db.exists(), "precondition: conflicting legacy db must exist"
        current_bytes_before = self.current_db.read_bytes()

        # Trigger the sweep; a per-roadmap conflict must be non-fatal.
        exit_code, _, stderr = self.test.run_cmd(["roadmap", "list"], check=False)
        assert exit_code == 0, (
            f"a conflict must be non-fatal, got exit {exit_code}, stderr={stderr!r}"
        )

        # project.db is NOT overwritten by the legacy file.
        assert self.current_db.read_bytes() == current_bytes_before, (
            "current project.db must NOT be overwritten on conflict"
        )
        # The legacy file is left untouched (not moved, deleted, or overwritten).
        assert self.legacy_db.exists(), "legacy file must be left in place on conflict"
        assert self.legacy_db.read_bytes() == legacy_marker + live_db_bytes, (
            "legacy file must be left byte-for-byte untouched on conflict"
        )
        # A non-fatal warning naming the roadmap is surfaced on stderr.
        assert self.roadmap in stderr, (
            f"conflict warning must name the roadmap on stderr, got {stderr!r}"
        )
        # The roadmap is still usable through the surviving current-layout db.
        listed = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        listed_titles = {t["title"] for t in listed}
        for title in self.task_titles:
            assert title in listed_titles, (
                f"current-layout data must remain intact on conflict; missing {title!r}"
            )
        print("✓ conflict on existing project.db is non-fatal; project.db not overwritten; legacy file untouched")


def _run_all():
    instance_cls = TestLayoutMigration
    method_names = [m for m in dir(instance_cls) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    for m in method_names:
        instance = instance_cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
        except Exception as exc:  # noqa: BLE001 - surface any unexpected error
            failed += 1
            failures.append((m, exc))
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Layout-migration tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
