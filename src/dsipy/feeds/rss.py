from rfeed import Feed, Item, Extension, Guid, Serializable
from ..crypto.signatures import sign_feed_item


def strip_cdata(value: str) -> str:
    """Return a description without its CDATA wrapper (the form that is signed)."""
    if value.startswith("<![CDATA[") and value.endswith("]]>"):
        return value[len("<![CDATA[") : -len("]]>")]
    return value


class RSSFeedAtomLinkNamespace(Extension):
    """Custom extension to add the Atom namespace to the RSS feed."""

    def get_namespace(self):
        return {"xmlns:atom": "http://www.w3.org/2005/Atom"}


class RSSFeedMediaNamespace(Extension):
    """Custom extension to declare the Media RSS namespace (media:content)."""

    def get_namespace(self):
        return {"xmlns:media": "http://search.yahoo.com/mrss/"}


class RSSFeedAtomLink(Serializable):
    """Custom tag for adding an Atom link to the RSS feed."""

    def __init__(self, href):
        Serializable.__init__(self)
        self.href = href

    def publish(self, handler):
        Serializable.publish(self, handler)
        self._write_element("atom:link", None, {"href": self.href, "rel": "self"})


class RSSFeedMediaContent(Serializable):
    """Custom tag for adding a media image to the RSS feed.
    <media:content
        url="https://example.com/video.mp4"
        type="video/mp4"
        medium="video"
        duration="91"
    >
    """

    def __init__(self, url):
        super().__init__()
        self.url = url
        self.type = None
        self.medium = None

        if url.endswith(".jpg") or url.endswith(".jpeg"):
            self.type = "image/jpeg"
            self.medium = "image"
        elif url.endswith(".png"):
            self.type = "image/png"
            self.medium = "image"
        elif url.endswith(".gif"):
            self.type = "image/gif"
            self.medium = "image"
        elif url.endswith(".webp"):
            self.type = "image/webp"
            self.medium = "image"
        elif url.endswith(".mp4"):
            self.type = "video/mp4"
            self.medium = "video"
        else:
            self.type = "application/octet-stream"
            self.medium = "unknown"

    def publish(self, handler):
        Serializable.publish(self, handler)
        self._write_element(
            "media:content",
            None,
            {"url": self.url, "type": self.type, "medium": self.medium},
        )


class RSSFeedSignature(Serializable):
    """
    Custom tag for adding a signature to an RSS item.
    """

    def __init__(self, signature_value, key_id):
        super().__init__()
        self.signature_value = signature_value
        self.key_id = key_id

    def publish(self, handler):
        super().publish(handler)
        self._write_element(
            "signature",
            self.signature_value,
            {"alg": "ed25519", "key-id": self.key_id},
        )


class RSSFeedItemCDATAPatch(Item):

    def _write_element(self, name, value, attributes=None):

        if attributes is None:
            attributes = {}
        if value is not None or attributes != {}:
            self.handler.startElement(name, attributes)

            if value is not None:
                """This is a patch to allow CDATA sections in the description field. If the value contains a CDATA section, we need to write it as raw XML instead of escaping it."""

                str_value = value if isinstance(value, str) else str(value)
                if name == "description" and value is not None:
                    cdata_start = str_value.find("<![CDATA[")
                    cdata_end = str_value.find("]]>")

                    if cdata_start > -1 and cdata_end > -1 and cdata_start < cdata_end:
                        self.handler.characters(str_value[:cdata_start])
                        self.handler._write(str_value[cdata_start : cdata_end + 3])
                        self.handler.characters(str_value[cdata_end + 3 :])
                    else:
                        self.handler.characters(str_value)
                else:
                    self.handler.characters(str_value)

            self.handler.endElement(name)


class RSSFeed:
    @staticmethod
    def build(
        title: str,
        link: str,
        description: str,
        author_name: str,
        author_email: str,
        language: str,
        build_date,
        items,
        sign=None,
    ):
        """Generate a feed from the given items and metadata."""

        feed_items = []
        for item in items:
            # https://docs.python.org/3/library/datetime.html#strftime-and-strptime-behavior
            id = item["id"]
            pub_date = item["date"]
            item_description = item.get("content")
            item_title = item.get("title")

            signature_value = None

            if sign:
                sign_key = sign.get("key")
                sign_id = sign.get("id")
                if not sign_key or not sign_id:
                    raise ValueError(
                        "Signing requires both a private 'key' and a key 'id'"
                    )
                signature_value = sign_feed_item(
                    sign_key,
                    # The signed pubDate must be the exact string published
                    # in the item (RFC section 4.2.1)
                    (pub_date if isinstance(pub_date, str) else Item._date(pub_date)),
                    item_title or "",
                    strip_cdata(item_description or ""),
                )

            item_extensions = []
            if signature_value:
                item_extensions.append(RSSFeedSignature(signature_value, sign_id))
            if item.get("image"):
                item_extensions.append(RSSFeedMediaContent(item["image"]))

            feed_items.append(
                RSSFeedItemCDATAPatch(
                    title=item_title,
                    link=item.get("link") or f"{link}/feed/{id}",
                    description=item_description,
                    author=f"{author_name} ({author_email})",
                    guid=Guid(id, False),
                    pubDate=pub_date,
                    extensions=item_extensions,
                )
            )
        feed = Feed(
            title=title,
            link=link,
            description=description,
            language=language,
            lastBuildDate=build_date,
            items=feed_items,
            extensions=[
                RSSFeedAtomLinkNamespace(),
                RSSFeedMediaNamespace(),
                RSSFeedAtomLink(link),
            ],
        )

        return feed.rss()

    def replace_template_variables(content: str, metadata: dict, vars_dict: dict):
        """
        Replace template variables in the content with values from metadata and vars_dict.
        Template variables are in the format {{ variable_name }}.
        The function first checks for the variable in metadata, then in vars_dict.
        If a variable is not found in either, it is left unchanged.
        Args:
            content (str): The content string containing template variables to replace.
            metadata (dict): A dictionary containing metadata values for replacement.
            vars_dict (dict): A dictionary containing additional variables for replacement.
        Returns:
            str: The content string with template variables replaced by their corresponding values.
        """
        replacements = {**metadata, **vars_dict}

        for key, value in replacements.items():
            if isinstance(value, str):
                content = content.replace(f"{{{{ {key} }}}}", str(value))

        return content
