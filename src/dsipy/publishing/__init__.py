from .base import PublishConflictError, PublishError, Publisher
from .github_pages import GitHubProvider
from .s3 import S3Provider


def get_publisher(provider_type: str, **kwargs) -> Publisher:
    """Factory function to get the appropriate feed publisher provider.

    Args:
        provider_type: Type of provider ('github' or 's3')
        **kwargs: Provider-specific arguments

    Returns:
        Publisher instance

    Raises:
        ValueError: If provider_type is not supported
    """
    provider_type = provider_type.lower()
    required = {"github": ("owner", "repo", "branch", "token"), "s3": ("bucket",)}
    missing = [k for k in required.get(provider_type, ()) if not kwargs.get(k)]
    if missing:
        raise ValueError(
            f"Provider '{provider_type}' is missing required argument(s): "
            f"{', '.join(missing)}"
        )
    if provider_type == "github":
        return GitHubProvider(
            owner=kwargs["owner"],
            repo=kwargs["repo"],
            branch=kwargs["branch"],
            token=kwargs["token"],
        )
    elif provider_type == "s3":
        return S3Provider(
            bucket=kwargs["bucket"],
            prefix=kwargs.get("prefix", ""),
            region=kwargs.get("region"),
        )
    else:
        raise ValueError(f"Unknown provider type: {provider_type}")
