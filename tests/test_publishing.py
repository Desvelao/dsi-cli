import base64
from unittest.mock import MagicMock, patch

import pytest
from botocore.exceptions import ClientError

from src.dsipy.publishing import get_publisher
from src.dsipy.publishing import github_pages, s3 as s3mod
from src.dsipy.publishing.base import PublishConflictError
from src.dsipy.publishing.github_pages import GitHubProvider
from src.dsipy.publishing.s3 import S3Provider


def resp(status=200, json=None, text="", content=b""):
    r = MagicMock()
    r.status_code = status
    r.json.return_value = json
    r.text = text
    r.content = content
    r.raise_for_status.side_effect = (
        None if status < 400 else RuntimeError(f"HTTP {status}")
    )
    return r


@pytest.fixture
def gh():
    return GitHubProvider("own er", "repo", "feat/x y", "tok")


def test_github_get_quotes_and_timeout(gh):
    body = {
        "content": base64.b64encode(b"hi").decode(),
        "encoding": "base64",
        "sha": "s1",
    }
    with patch.object(github_pages.requests, "get", return_value=resp(json=body)) as g:
        assert gh.get_remote("dir/a b.xml") == ("hi", "s1")
    args, kw = g.call_args
    assert args[0].endswith("/own%20er/repo/contents/dir/a%20b.xml")
    assert kw["params"] == {"ref": "feat/x y"}
    assert kw["timeout"] == github_pages.TIMEOUT


def test_github_get_missing(gh):
    with patch.object(github_pages.requests, "get", return_value=resp(404)):
        assert gh.get_remote("a.xml") == (None, None)


def test_github_large_file_falls_back_to_raw(gh):
    first = resp(json={"content": "", "encoding": "none", "sha": "big"})
    raw = resp(content=b"x" * 5)
    with patch.object(github_pages.requests, "get", side_effect=[first, raw]) as g:
        assert gh.get_remote("a.xml") == ("xxxxx", "big")
    kw = g.call_args_list[1].kwargs
    assert kw["headers"]["Accept"] == github_pages.RAW_ACCEPT
    assert kw["timeout"] == github_pages.TIMEOUT


def test_github_publish_payload_and_timeout(gh):
    with patch.object(
        github_pages.requests, "put", return_value=resp(json={"ok": 1})
    ) as p:
        assert gh.publish("a.xml", "c", "oldsha") == {"ok": 1}
    kw = p.call_args.kwargs
    assert kw["timeout"] == github_pages.TIMEOUT
    assert kw["json"]["sha"] == "oldsha"
    assert kw["json"]["branch"] == "feat/x y"


@pytest.mark.parametrize(
    "status,text", [(409, "conflict"), (422, '{"message":"a.xml does not match sha"}')]
)
def test_github_conflict(gh, status, text):
    with patch.object(
        github_pages.requests, "put", return_value=resp(status, text=text)
    ):
        with pytest.raises(PublishConflictError):
            gh.publish("a.xml", "c", "old")


def test_github_other_422_not_conflict(gh):
    with patch.object(github_pages.requests, "put", return_value=resp(422, text="bad")):
        with pytest.raises(RuntimeError):
            gh.publish("a.xml", "c", None)


@pytest.fixture
def s3p():
    with patch.object(s3mod.boto3, "client") as c:
        client = c.return_value
        client.exceptions.NoSuchKey = type("NoSuchKey", (Exception,), {})
        yield S3Provider("b", prefix="feeds", region="eu"), client


@pytest.mark.parametrize(
    "prefix,path,key",
    [
        ("", "a.xml", "a.xml"),
        ("", "/a.xml", "a.xml"),
        ("feeds", "a.xml", "feeds/a.xml"),
        ("feeds/", "a.xml", "feeds/a.xml"),
        ("/feeds/", "/a.xml", "feeds/a.xml"),
    ],
)
def test_s3_key_join(prefix, path, key):
    with patch.object(s3mod.boto3, "client"):
        assert S3Provider("b", prefix=prefix)._key(path) == key


def test_s3_publish_ifmatch_and_content_type(s3p):
    p, client = s3p
    p.publish("a.xml", "c", "abc")
    kw = client.put_object.call_args.kwargs
    assert kw["IfMatch"] == '"abc"'
    assert "IfNoneMatch" not in kw
    assert kw["Key"] == "feeds/a.xml"
    assert kw["ContentType"] == "application/xml"


def test_s3_publish_create_uses_if_none_match(s3p):
    p, client = s3p
    p.publish("a.json", "{}", None)
    kw = client.put_object.call_args.kwargs
    assert kw["IfNoneMatch"] == "*"
    assert kw["ContentType"] == "application/json"


def test_s3_precondition_failed_maps_to_conflict(s3p):
    p, client = s3p
    client.put_object.side_effect = ClientError(
        {"Error": {"Code": "PreconditionFailed"}}, "PutObject"
    )
    with pytest.raises(PublishConflictError):
        p.publish("a.xml", "c", "old")


def test_s3_other_client_error_propagates(s3p):
    p, client = s3p
    client.put_object.side_effect = ClientError(
        {"Error": {"Code": "AccessDenied"}}, "PutObject"
    )
    with pytest.raises(ClientError):
        p.publish("a.xml", "c", "old")


def test_s3_get_remote(s3p):
    p, client = s3p
    body = MagicMock()
    body.read.return_value = b"hi"
    client.get_object.return_value = {"Body": body, "ETag": '"e1"'}
    assert p.get_remote("a.xml") == ("hi", "e1")
    client.get_object.side_effect = client.exceptions.NoSuchKey()
    assert p.get_remote("a.xml") == (None, None)


def test_get_publisher_missing_args():
    with pytest.raises(ValueError, match=r"github.*repo.*token"):
        get_publisher("github", owner="o", branch="b")
    with pytest.raises(ValueError, match="s3.*bucket"):
        get_publisher("s3")
    with pytest.raises(ValueError, match="Unknown"):
        get_publisher("ftp")


def test_get_publisher_ok():
    assert isinstance(
        get_publisher("github", owner="o", repo="r", branch="b", token="t"),
        GitHubProvider,
    )
    with patch.object(s3mod.boto3, "client"):
        assert isinstance(get_publisher("S3", bucket="b"), S3Provider)
