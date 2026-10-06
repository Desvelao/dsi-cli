import datetime
import os
import tempfile
import unittest
from pathlib import Path

from src.dsipy.feeds.markdown import FeedFileError, MarkdownFeed


class TestMarkdownFeed(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)

    def write(self, text, name="post.md"):
        p = self.dir / name
        p.write_text(text, encoding="utf-8")
        return p

    def date_of(self, value):
        p = self.write(f"---\ntitle: T\ndate: {value}\n---\nbody\n")
        return MarkdownFeed._parse_file(p)["date"]

    def test_date_forms(self):
        D = datetime.datetime
        cases = {
            "2025-01-02": D(2025, 1, 2),
            "2025-01-02T03:04:05Z": D(2025, 1, 2, 3, 4, 5),
            "2025-01-02T03:04:05+02:00": D(2025, 1, 2, 1, 4, 5),
            "2025-01-02T03:04:05.250Z": D(2025, 1, 2, 3, 4, 5, 250000),
            "2025-01-02 03:04": D(2025, 1, 2, 3, 4),
            "2025-01-02T03:04:05-05:00": D(2025, 1, 2, 8, 4, 5),
        }
        for value, expected in cases.items():
            with self.subTest(value=value):
                self.assertEqual(self.date_of(value), expected)

    def test_invalid_date_names_file(self):
        p = self.write("---\ndate: not-a-date\n---\nx\n", "bad.md")
        with self.assertRaises(FeedFileError) as ctx:
            MarkdownFeed._parse_file(p)
        self.assertIn(str(p), str(ctx.exception))
        self.assertIn("not-a-date", str(ctx.exception))

    def test_collect_reports_offending_file(self):
        self.write("---\ndate: 2025-01-01\n---\nok\n", "good.md")
        bad = self.write("---\ndate: 31/12/2025\n---\nx\n", "bad.md")
        with self.assertRaises(ValueError) as ctx:
            MarkdownFeed.collect(str(self.dir))
        self.assertIn(str(bad), str(ctx.exception))

    def test_fallback_date_is_mtime_in_utc(self):
        p = self.write("no front matter\n")
        ts = 1735787045  # 2025-01-02T03:04:05Z
        os.utime(p, (ts, ts))
        item = MarkdownFeed._parse_file(p)
        self.assertEqual(item["date"], datetime.datetime(2025, 1, 2, 3, 4, 5))
        self.assertEqual(item["metadata"]["date"], "2025-01-02T03:04:05Z")

    def test_fallback_date_independent_of_local_timezone(self):
        p = self.write("x\n")
        ts = 1735787045
        os.utime(p, (ts, ts))
        old = os.environ.get("TZ")
        self.addCleanup(
            lambda: (
                os.environ.pop("TZ", None) if old is None else os.environ.update(TZ=old)
            )
        )
        import time

        self.addCleanup(time.tzset)
        os.environ["TZ"] = "Asia/Tokyo"
        time.tzset()
        self.assertEqual(
            MarkdownFeed._parse_file(p)["metadata"]["date"], "2025-01-02T03:04:05Z"
        )

    def test_unterminated_front_matter_is_an_error_naming_file(self):
        p = self.write("---\ntitle: T\nbody text never closed\n", "open.md")
        with self.assertRaises(FeedFileError) as ctx:
            MarkdownFeed._parse_file(p)
        self.assertIn(str(p), str(ctx.exception))
        self.assertIn("unterminated", str(ctx.exception))

    def test_quotes_are_stripped(self):
        p = self.write(
            "---\ntitle: \"Hello: World\"\nlink: 'https://e.com/x'\n"
            'date: "2025-01-02"\nother: "mismatch\'\n---\nb\n'
        )
        item = MarkdownFeed._parse_file(p)
        self.assertEqual(item["title"], "Hello: World")
        self.assertEqual(item["link"], "https://e.com/x")
        self.assertEqual(item["date"], datetime.datetime(2025, 1, 2))
        self.assertEqual(item["metadata"]["other"], "\"mismatch'")

    def test_valid_front_matter_content(self):
        p = self.write("---\ntitle: T\n---\nline1\nline2\n")
        self.assertEqual(MarkdownFeed._parse_file(p)["content"], "line1\nline2")


class TestItemId(unittest.TestCase):
    def make_root(self, base):
        root = Path(base) / "feed"
        (root / "2025").mkdir(parents=True)
        (root / "a.md").write_text("---\ntitle: A\ndate: 2025-01-01\n---\nx\n")
        (root / "2025" / "News Post.md").write_text(
            "---\ntitle: B\ndate: 2025-01-02\n---\nx\n"
        )
        (root / "c.md").write_text(
            "---\ntitle: C\ndate: 2025-01-03\nid: custom\n---\nx\n"
        )
        return root

    def ids(self, target):
        return {s["title"]: s["id"] for s in MarkdownFeed.collect(str(target))}

    def test_ids_independent_of_root_and_cwd(self):
        with tempfile.TemporaryDirectory() as t1, tempfile.TemporaryDirectory() as t2:
            r1, r2 = self.make_root(t1), self.make_root(Path(t2) / "deep")
            cwd = os.getcwd()
            try:
                os.chdir(t1)
                ids1 = self.ids(r1)
                os.chdir(t2)
                ids2 = self.ids(r2)
                rel = self.ids("deep/feed")
            finally:
                os.chdir(cwd)
        self.assertEqual(ids1, ids2)
        self.assertEqual(ids1, rel)

    def test_nested_and_override(self):
        with tempfile.TemporaryDirectory() as t:
            ids = self.ids(self.make_root(t))
        self.assertEqual(ids, {"A": "a", "B": "2025-news-post", "C": "custom"})

    def test_single_file_uses_stem(self):
        with tempfile.TemporaryDirectory() as t:
            root = self.make_root(t)
            states = MarkdownFeed.collect(str(root / "2025" / "News Post.md"))
        self.assertEqual([s["id"] for s in states], ["news-post"])


if __name__ == "__main__":
    unittest.main()
