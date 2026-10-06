import base64
from typing import Optional
from urllib.parse import quote

import requests

from .base import PublishConflictError, Publisher

TIMEOUT = 30  # seconds
RAW_ACCEPT = "application/vnd.github.raw+json"


class GitHubProvider(Publisher):
    def __init__(self, owner: str, repo: str, branch: str, token: str):
        self.owner = owner
        self.repo = repo
        self.branch = branch
        self.token = token
        self.headers = {"Authorization": f"token {token}"}

    def _url(self, path: str):
        return (
            f"https://api.github.com/repos/{quote(self.owner, safe='')}"
            f"/{quote(self.repo, safe='')}/contents/{quote(path.lstrip('/'), safe='/')}"
        )

    def get_remote(self, path: str):
        url = self._url(path)
        params = {"ref": self.branch}
        resp = requests.get(
            url, headers=self.headers, params=params, timeout=TIMEOUT
        )
        if resp.status_code == 404:
            return None, None
        resp.raise_for_status()
        data = resp.json()
        encoded = data.get("content")
        if encoded and data.get("encoding", "base64") == "base64":
            content = base64.b64decode(encoded).decode("utf-8")
        else:
            # Files over 1MB come back with empty content: fetch the raw body.
            raw = requests.get(
                url,
                headers={**self.headers, "Accept": RAW_ACCEPT},
                params=params,
                timeout=TIMEOUT,
            )
            raw.raise_for_status()
            content = raw.content.decode("utf-8")
        return content, data["sha"]

    def publish(self, path: str, content: str, version: Optional[str]):
        url = self._url(path)
        payload = {
            "message": f"Update feed: {path}",
            "content": base64.b64encode(content.encode("utf-8")).decode("utf-8"),
            "branch": self.branch,
        }
        if version:
            payload["sha"] = version
        resp = requests.put(
            url, headers=self.headers, json=payload, timeout=TIMEOUT
        )
        if resp.status_code == 409 or (
            resp.status_code == 422 and "sha" in resp.text.lower()
        ):
            raise PublishConflictError(
                f"GitHub: {path} changed on '{self.branch}' since it was read "
                f"(HTTP {resp.status_code}); re-run to fetch the latest version."
            )
        resp.raise_for_status()
        return resp.json()
