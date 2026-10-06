import pytest
from typer.testing import CliRunner

from src.dsipy.cli import feeds
from src.dsipy.cli.app import main_app
from src.dsipy.publishing import PublishConflictError, PublishError

runner = CliRunner()


class FakePublisher:
    def __init__(self, remote=None, fail=(), conflict=()):
        self.remote = dict(remote or {})
        self.fail = set(fail)
        self.conflict = set(conflict)
        self.published = {}
        self.attempted = []

    def get_remote(self, path):
        if path in self.remote:
            return self.remote[path], "v1"
        return None, None

    def publish(self, path, content, version):
        self.attempted.append(path)
        if path in self.fail:
            raise PublishError("boom")
        if path in self.conflict:
            raise PublishConflictError("stale")
        self.published[path] = content


@pytest.fixture
def fake(monkeypatch):
    holder = {}

    def install(**kw):
        holder["p"] = FakePublisher(**kw)
        monkeypatch.setattr(feeds, "get_publisher", lambda *a, **k: holder["p"])
        return holder["p"]

    return install


def run(*args):
    return runner.invoke(main_app, ["feeds", "publish", *args])


def tree(tmp_path):
    (tmp_path / "a").mkdir()
    (tmp_path / "b").mkdir()
    (tmp_path / "a" / "feed.rss").write_text("A", encoding="utf-8")
    (tmp_path / "b" / "feed.rss").write_text("B", encoding="utf-8")
    (tmp_path / "top.xml").write_text("T", encoding="utf-8")
    (tmp_path / "ignore.txt").write_text("x")
    (tmp_path / "old.atom").write_text("x")
    return tmp_path


def test_nested_relative_paths_no_collision(tmp_path, fake):
    p = fake()
    r = run(str(tree(tmp_path)), "--provider", "github")
    assert r.exit_code == 0, r.output
    assert p.published == {"a/feed.rss": "A", "b/feed.rss": "B", "top.xml": "T"}
    assert "Done." in r.output


def test_prefix_without_trailing_slash(tmp_path, fake):
    p = fake()
    r = run(str(tree(tmp_path)), "--provider", "github", "--prefix", "feeds")
    assert r.exit_code == 0
    assert set(p.published) == {"feeds/a/feed.rss", "feeds/b/feed.rss", "feeds/top.xml"}
    p2 = fake()
    run(str(tmp_path), "--provider", "github", "--prefix", "feeds/")
    assert "feeds/top.xml" in p2.published


def test_single_file_uses_name(tmp_path, fake):
    p = fake()
    f = tmp_path / "x.rss"
    f.write_text("X", encoding="utf-8")
    assert run(str(f), "--provider", "s3", "--prefix", "p").exit_code == 0
    assert p.published == {"p/x.rss": "X"}


def test_failure_exit_code_and_continues(tmp_path, fake):
    p = fake(fail={"a/feed.rss"}, conflict={"b/feed.rss"})
    r = run(str(tree(tmp_path)), "--provider", "github")
    assert r.exit_code == 1
    assert p.attempted == ["a/feed.rss", "b/feed.rss", "top.xml"]
    assert p.published == {"top.xml": "T"}
    assert "Done." not in r.output
    assert "Conflict" in r.output
    assert "2 file(s) failed" in r.output


def test_undecodable_file_counted_failed(tmp_path, fake):
    p = fake()
    (tmp_path / "bad.rss").write_bytes(b"\xff\xfe\x00bad\xff")
    (tmp_path / "good.rss").write_text("é", encoding="utf-8")
    r = run(str(tmp_path), "--provider", "github")
    assert r.exit_code == 1
    assert p.published == {"good.rss": "é"}
    assert "Failed to read" in r.output


def test_dry_run(tmp_path, fake):
    p = fake(remote={"top.xml": "T"})
    r = run(str(tree(tmp_path)), "--provider", "github", "--dry-run")
    assert r.exit_code == 0
    assert p.attempted == []
    assert "would publish" in r.output.lower()
    assert "Would publish: 2" in r.output
    assert "Published:" not in r.output
    assert "Unchanged:" in r.output


def test_no_feed_files(tmp_path, fake):
    fake()
    (tmp_path / "a.atom").write_text("x")
    assert run(str(tmp_path), "--provider", "github").exit_code == 1


def test_unknown_provider(tmp_path):
    (tmp_path / "f.rss").write_text("x")
    r = run(str(tmp_path), "--provider", "webdav")
    assert r.exit_code == 1
    assert "Unknown provider type: webdav" in r.output


def test_missing_provider_args(tmp_path):
    (tmp_path / "f.rss").write_text("x")
    r = run(str(tmp_path), "--provider", "github", "--arg", "owner=me")
    assert r.exit_code == 1
    assert "missing required argument(s): repo, branch, token" in r.output


def test_bad_arg_format(tmp_path):
    (tmp_path / "f.rss").write_text("x")
    r = run(str(tmp_path), "--provider", "s3", "--arg", "oops")
    assert r.exit_code != 0
    assert "key=value" in r.output


def test_help_lists_supported_providers():
    r = runner.invoke(main_app, ["feeds", "publish", "--help"])
    assert "webdav" not in r.output
