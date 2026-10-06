import mimetypes
from typing import Optional

import boto3
from botocore.exceptions import ClientError

from .base import PublishConflictError, Publisher

_CONFLICT_CODES = {"PreconditionFailed", "ConditionalRequestConflict", "412", "409"}


class S3Provider(Publisher):
    def __init__(self, bucket: str, prefix: str = "", region: str = None):
        self.bucket = bucket
        self.prefix = prefix
        self.s3 = boto3.client("s3", region_name=region)

    def _key(self, path: str):
        prefix = (self.prefix or "").strip("/")
        path = path.lstrip("/")
        return f"{prefix}/{path}" if prefix else path

    @staticmethod
    def _content_type(key: str) -> str:
        if key.lower().endswith((".xml", ".rss", ".opml")):
            return "application/xml"
        return mimetypes.guess_type(key)[0] or "application/xml"

    def get_remote(self, path: str):
        key = self._key(path)
        try:
            obj = self.s3.get_object(Bucket=self.bucket, Key=key)
            content = obj["Body"].read().decode("utf-8")
            etag = obj["ETag"].strip('"')
            return content, etag
        except self.s3.exceptions.NoSuchKey:
            return None, None

    def publish(self, path: str, content: str, version: Optional[str]):
        key = self._key(path)
        kwargs = {}
        if version:
            kwargs["IfMatch"] = f'"{version}"'
        else:
            # Create only if the object does not exist yet.
            kwargs["IfNoneMatch"] = "*"
        try:
            return self.s3.put_object(
                Bucket=self.bucket,
                Key=key,
                Body=content.encode("utf-8"),
                ContentType=self._content_type(key),
                **kwargs,
            )
        except ClientError as e:
            code = str(e.response.get("Error", {}).get("Code", ""))
            status = str(
                e.response.get("ResponseMetadata", {}).get("HTTPStatusCode", "")
            )
            if code in _CONFLICT_CODES or status in ("412", "409"):
                raise PublishConflictError(
                    f"S3: s3://{self.bucket}/{key} changed since it was read "
                    f"({code}); re-run to fetch the latest version."
                ) from e
            raise
