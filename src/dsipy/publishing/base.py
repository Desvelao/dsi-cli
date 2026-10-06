from abc import ABC, abstractmethod
from typing import Optional


class PublishError(Exception):
    """Base class for publishing errors."""


class PublishConflictError(PublishError):
    """The remote file changed since it was read (stale version/sha/ETag)."""


class Publisher(ABC):
    """Base class for feed publishing providers."""

    @abstractmethod
    def get_remote(self, path: str) -> tuple[Optional[str], Optional[str]]:
        """Return (content, version/etag) or (None, None) if missing."""
        pass

    @abstractmethod
    def publish(self, path: str, content: str, version: Optional[str]):
        """Publish content to remote provider.

        Raises PublishConflictError if `version` no longer matches the remote.
        """
        pass
