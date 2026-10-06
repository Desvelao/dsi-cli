import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import urllib3

from src.dsipy.core.identity import VCard
from src.dsipy.core.http import FetchError, fetch_text
from src.dsipy.core.resolver import (
    SourceMismatchError,
    fetch_save_vcard_from_url,
    fetch_vcard_from_url,
    normalize_url,
    safe_filename,
    source_matches,
)

PUBLIC_IP = "93.184.216.34"


class FakeResponse:
    def __init__(self, status=200, body=b"", headers=None):
        self.status_code = status
        self.ok = status < 400
        self.headers = headers or {}
        self._body = body

    def iter_content(self, chunk_size=8192):
        for i in range(0, len(self._body), chunk_size):
            yield self._body[i : i + chunk_size]

    def close(self):
        pass


def vcard(source):
    return f"BEGIN:VCARD\nVERSION:4.0\nFN:Alice\nSOURCE:{source}\nEND:VCARD\n"


class TestFetchText(unittest.TestCase):
    def setUp(self):
        patcher = patch(
            "src.dsipy.core.http._resolve_addresses", return_value=[PUBLIC_IP]
        )
        self.resolve = patcher.start()
        self.addCleanup(patcher.stop)

    def test_https_is_fetched(self):
        with patch(
            "src.dsipy.core.http.requests.Session.get",
            return_value=FakeResponse(
                body=b"hello", headers={"Content-Type": "text/plain"}
            ),
        ):
            self.assertEqual(fetch_text("https://example.com/a").text, "hello")

    def test_plain_http_is_refused_by_default(self):
        with patch("src.dsipy.core.http.requests.Session.get") as get:
            with self.assertRaises(FetchError):
                fetch_text("http://example.com/a")
            get.assert_not_called()

    def test_plain_http_can_be_allowed(self):
        with patch(
            "src.dsipy.core.http.requests.Session.get", return_value=FakeResponse(body=b"x")
        ):
            self.assertEqual(
                fetch_text("http://example.com/a", allow_http=True).text, "x"
            )

    def test_other_schemes_are_refused(self):
        for url in ("file:///etc/passwd", "ftp://example.com/a", "gopher://x/"):
            with self.assertRaises(FetchError):
                fetch_text(url, allow_http=True)

    def test_private_and_loopback_addresses_are_refused(self):
        for address in (
            "127.0.0.1",
            "10.0.0.5",
            "192.168.1.1",
            "169.254.169.254",
            "::1",
        ):
            self.resolve.return_value = [address]
            with patch("src.dsipy.core.http.requests.Session.get") as get:
                with self.assertRaises(FetchError, msg=address):
                    fetch_text("https://internal.example/a")
                get.assert_not_called()

    def test_host_with_one_private_address_is_refused(self):
        self.resolve.return_value = [PUBLIC_IP, "10.0.0.5"]
        with self.assertRaises(FetchError):
            fetch_text("https://example.com/a")

    def test_credentials_in_url_are_refused(self):
        with self.assertRaises(FetchError):
            fetch_text("https://user:pass@example.com/a")

    def test_redirect_to_a_private_address_is_refused(self):
        def resolve(host, port):
            return ["127.0.0.1"] if host == "internal.example" else [PUBLIC_IP]

        self.resolve.side_effect = resolve
        redirect = FakeResponse(302, headers={"Location": "https://internal.example/x"})
        with patch("src.dsipy.core.http.requests.Session.get", return_value=redirect) as get:
            with self.assertRaises(FetchError):
                fetch_text("https://example.com/a")
            self.assertEqual(get.call_count, 1)

    def test_redirect_limit(self):
        redirect = FakeResponse(302, headers={"Location": "https://example.com/next"})
        with patch("src.dsipy.core.http.requests.Session.get", return_value=redirect):
            with self.assertRaises(FetchError):
                fetch_text("https://example.com/a", max_redirects=3)

    def test_relative_redirect_is_followed(self):
        responses = [
            FakeResponse(301, headers={"Location": "/b"}),
            FakeResponse(body=b"done"),
        ]
        with patch("src.dsipy.core.http.requests.Session.get", side_effect=responses):
            result = fetch_text("https://example.com/a")
        self.assertEqual(result.url, "https://example.com/b")

    def test_response_size_limit(self):
        big = FakeResponse(body=b"a" * 5000)
        with patch("src.dsipy.core.http.requests.Session.get", return_value=big):
            with self.assertRaises(FetchError):
                fetch_text("https://example.com/a", max_bytes=1000)

    def test_declared_content_length_over_limit(self):
        big = FakeResponse(body=b"a", headers={"Content-Length": "999999"})
        with patch("src.dsipy.core.http.requests.Session.get", return_value=big):
            with self.assertRaises(FetchError):
                fetch_text("https://example.com/a", max_bytes=1000)

    def test_html_content_type_is_refused(self):
        page = FakeResponse(body=b"<html>", headers={"Content-Type": "text/html"})
        with patch("src.dsipy.core.http.requests.Session.get", return_value=page):
            with self.assertRaises(FetchError):
                fetch_text("https://example.com/a", accepted_types=("text/vcard",))

    def test_http_error_status(self):
        with patch("src.dsipy.core.http.requests.Session.get", return_value=FakeResponse(404)):
            with self.assertRaises(FetchError):
                fetch_text("https://example.com/a")


class TestPinnedConnection(unittest.TestCase):
    def fetch_capturing_pool(self, url="https://example.com/a"):
        seen = {}

        def urlopen(pool, method=None, url=None, **kwargs):
            seen.update(
                host=pool.host,
                path=url,
                headers=kwargs.get("headers"),
                server_hostname=getattr(pool, "server_hostname", None) or pool.conn_kw.get("server_hostname"),
                assert_hostname=getattr(pool, "assert_hostname", None),
            )
            raise urllib3.exceptions.ProtocolError("stop")

        with patch("urllib3.connectionpool.HTTPConnectionPool.urlopen", urlopen):
            with self.assertRaises(FetchError):
                fetch_text(url)
        return seen

    def test_connection_uses_the_validated_ip_after_rebinding(self):
        # Public at check time, private on any later lookup (DNS rebinding).
        answers = iter([[PUBLIC_IP]])
        with patch(
            "src.dsipy.core.http._resolve_addresses",
            side_effect=lambda h, p: next(answers, ["127.0.0.1"]),
        ), patch(
            "socket.getaddrinfo", return_value=[(2, 1, 6, "", ("127.0.0.1", 443))]
        ):
            seen = self.fetch_capturing_pool()
        self.assertEqual(seen["host"], PUBLIC_IP)
        self.assertEqual(seen["headers"]["Host"], "example.com")
        self.assertEqual(seen["server_hostname"], "example.com")
        self.assertEqual(seen["assert_hostname"], "example.com")

    def test_host_header_keeps_non_default_port(self):
        with patch("src.dsipy.core.http._resolve_addresses", return_value=[PUBLIC_IP]):
            seen = self.fetch_capturing_pool("https://example.com:8443/a")
        self.assertEqual(seen["headers"]["Host"], "example.com:8443")

    def test_proxy_environment_is_ignored(self):
        env = {
            "HTTPS_PROXY": "http://proxy.internal:3128",
            "HTTP_PROXY": "http://proxy.internal:3128",
            "https_proxy": "http://proxy.internal:3128",
        }
        with patch.dict("os.environ", env), patch(
            "src.dsipy.core.http._resolve_addresses", return_value=[PUBLIC_IP]
        ):
            seen = self.fetch_capturing_pool()
        self.assertEqual(seen["host"], PUBLIC_IP)

    def test_session_does_not_trust_env(self):
        from src.dsipy.core.http import _pinned_session

        self.assertFalse(_pinned_session("example.com", 443, PUBLIC_IP).trust_env)


class TestNormalizeAndSource(unittest.TestCase):
    def test_normalization(self):
        self.assertEqual(
            normalize_url("HTTPS://Example.COM:443/alice.vcf#frag"),
            "https://example.com/alice.vcf",
        )
        self.assertEqual(normalize_url("https://example.com."), "https://example.com/")
        self.assertEqual(
            normalize_url("http://example.com:8080/a?b=1"),
            "http://example.com:8080/a?b=1",
        )

    def test_percent_encoding_normalization(self):
        self.assertEqual(normalize_url("https://e.com/%7Ealice"), "https://e.com/~alice")
        self.assertEqual(normalize_url("https://e.com/a%2fb"), "https://e.com/a%2Fb")
        self.assertEqual(normalize_url("https://e.com/a?x=%2f"), "https://e.com/a?x=%2F")

    def test_dot_segments(self):
        self.assertEqual(normalize_url("https://e.com/a/./b/../c"), "https://e.com/a/c")
        self.assertEqual(normalize_url("https://e.com/a/b/.."), "https://e.com/a/")
        self.assertEqual(normalize_url("https://e.com/../a"), "https://e.com/a")

    def test_invalid_port_raises_value_error(self):
        for bad in ("https://e.com:abc/a", "https://e.com:99999/a"):
            with self.assertRaises(ValueError):
                normalize_url(bad)

    def test_source_matches_equivalent_encodings(self):
        self.assertTrue(
            source_matches("https://e.com/%7Ea/./b%2f", "https://e.com/~a/b%2F")
        )

    def test_source_matches_invalid_port_is_mismatch(self):
        for bad in ("https://e.com:abc/a", "https://e.com:99999/a"):
            self.assertFalse(source_matches("https://e.com/a", bad))

    def test_source_matches(self):
        self.assertTrue(
            source_matches(
                "https://example.com/alice.vcf", "https://EXAMPLE.com:443/alice.vcf"
            )
        )
        self.assertFalse(
            source_matches(
                "https://alice.example/dsi.vcf", "https://evil.example/bob.vcf"
            )
        )
        self.assertFalse(source_matches("https://example.com/a", None))


class TestFetchVcard(unittest.TestCase):
    def setUp(self):
        patcher = patch(
            "src.dsipy.core.http._resolve_addresses", return_value=[PUBLIC_IP]
        )
        patcher.start()
        self.addCleanup(patcher.stop)

    def fetch(self, url, text, **kwargs):
        response = FakeResponse(
            body=text.encode(), headers={"Content-Type": "text/vcard"}
        )
        with patch("src.dsipy.core.http.requests.Session.get", return_value=response):
            return fetch_vcard_from_url(url, **kwargs)

    def test_matching_source_is_accepted(self):
        url = "https://alice.example/dsi.vcf"
        text, filename = self.fetch(url, vcard(url))
        self.assertIn("FN:Alice", text)
        self.assertEqual(filename, "dsi.vcf")

    def test_identity_substitution_is_rejected(self):
        with self.assertRaises(SourceMismatchError):
            self.fetch(
                "https://alice.example/dsi.vcf", vcard("https://evil.example/bob.vcf")
            )

    def test_invalid_source_port_is_rejected(self):
        for bad in ("https://alice.example:abc/dsi.vcf", "https://alice.example:99999/x"):
            with self.assertRaises(SourceMismatchError):
                self.fetch("https://alice.example/dsi.vcf", vcard(bad))

    def test_missing_source_is_rejected(self):
        with self.assertRaises(SourceMismatchError):
            self.fetch(
                "https://alice.example/dsi.vcf",
                "BEGIN:VCARD\nVERSION:4.0\nFN:A\nEND:VCARD\n",
            )

    def test_verification_can_be_disabled(self):
        text, _ = self.fetch(
            "https://alice.example/dsi.vcf",
            vcard("https://evil.example/bob.vcf"),
            verify_source=False,
        )
        self.assertIn("evil.example", text)

    def test_non_vcard_is_rejected(self):
        with self.assertRaises(FetchError):
            self.fetch("https://alice.example/dsi.vcf", "<html></html>")


class TestSafeFilename(unittest.TestCase):
    def test_path_traversal_is_stripped(self):
        self.assertEqual(
            safe_filename('attachment; filename="../../etc/cron.d/x"', "https://a/b"),
            "x",
        )
        self.assertEqual(
            safe_filename('attachment; filename="..\\..\\evil.vcf"', "https://a/b"),
            "evil.vcf",
        )

    def test_falls_back_to_url_name_then_default(self):
        self.assertEqual(safe_filename("", "https://a/path/alice.vcf"), "alice.vcf")
        self.assertEqual(safe_filename("", "https://a/"), "vcard.vcf")
        self.assertEqual(
            safe_filename('attachment; filename=".."', "https://a/"), "vcard.vcf"
        )


if __name__ == "__main__":
    unittest.main()


class TestVCardToFile(unittest.TestCase):
    def test_url_initialised_card_can_be_saved(self):
        url = "https://alice.example/dsi.vcf"
        response = FakeResponse(
            body=vcard(url).encode(), headers={"Content-Type": "text/vcard"}
        )
        with tempfile.TemporaryDirectory() as tmp, patch(
            "src.dsipy.core.http._resolve_addresses", return_value=[PUBLIC_IP]
        ), patch("src.dsipy.core.http.requests.Session.get", return_value=response):
            card = VCard(url=url)
            self.assertIsInstance(card.path, Path)
            target = Path(tmp) / card.path.name
            card.to_file(target)
            self.assertIn("FN:Alice", target.read_text(encoding="utf-8"))

    def test_empty_text_is_not_missing_input(self):
        VCard(text="")


class TestFetchSaveOverwrite(unittest.TestCase):
    URL = "https://example.com/alice.vcf"

    def save(self, directory, **kwargs):
        with patch(
            "src.dsipy.core.resolver.fetch_vcard_from_url",
            return_value=(vcard(self.URL), "alice.vcf"),
        ):
            return fetch_save_vcard_from_url(self.URL, Path(directory), **kwargs)

    def test_new_file_is_written(self):
        with tempfile.TemporaryDirectory() as d:
            destination, text = self.save(d)
            self.assertEqual(destination.read_text(), text)

    def test_existing_file_is_refused_by_default(self):
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "alice.vcf").write_text("old")
            with self.assertRaises(FileExistsError):
                self.save(d)
            self.assertEqual((Path(d) / "alice.vcf").read_text(), "old")

    def test_overwrite_replaces_existing_file(self):
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "alice.vcf").write_text("old")
            destination, text = self.save(d, overwrite=True)
            self.assertEqual(destination.read_text(), text)
