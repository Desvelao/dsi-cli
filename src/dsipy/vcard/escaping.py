"""TEXT value escaping as defined in RFC 6350 section 3.4."""

import re

_ESCAPE_RE = re.compile(r"\\(.)", re.DOTALL)


def escape_text(value: str) -> str:
    """Escape a TEXT value so it is safe to place in a vCard property."""
    return (
        value.replace("\\", "\\\\")
        .replace("\r\n", "\\n")
        .replace("\n", "\\n")
        .replace("\r", "\\n")
        .replace(",", "\\,")
        .replace(";", "\\;")
    )


def unescape_text(value: str) -> str:
    """Reverse escape_text (also accepts \\N for newline)."""

    def replace(match: re.Match) -> str:
        char = match.group(1)
        return "\n" if char in ("n", "N") else char

    return _ESCAPE_RE.sub(replace, value)


def escape_component(value) -> str:
    """Escape one component of a structured or list value (N, ADR, CATEGORIES)."""
    return escape_text(str(value))


def join_structured(value, separator: str, size: int = None) -> str:
    """Build a structured (``;``) or list (``,``) value.

    ``value`` is either a str, taken as already structured text whose
    ``separator`` characters are kept as separators (only backslashes and line
    breaks are escaped), or a list/tuple of components that are escaped one by
    one, so separators inside a component stay literal. When ``size`` is given
    the result is padded with empty components up to that many components.
    """
    if isinstance(value, (list, tuple)):
        parts = [escape_component(v) for v in value]
    else:
        text = (
            str(value)
            .replace("\\", "\\\\")
            .replace("\r\n", "\\n")
            .replace("\n", "\\n")
            .replace("\r", "\\n")
        )
        other = "," if separator == ";" else ";"
        text = text.replace(other, "\\" + other)
        parts = text.split(separator)
    if size is not None and len(parts) < size:
        parts += [""] * (size - len(parts))
    return separator.join(parts)


def split_structured(value: str, separator: str) -> list:
    """Split a structured/list value on unescaped separators and unescape parts."""
    parts, current, i = [], [], 0
    while i < len(value):
        char = value[i]
        if char == "\\" and i + 1 < len(value):
            current.append(value[i : i + 2])
            i += 2
            continue
        if char == separator:
            parts.append("".join(current))
            current = []
        else:
            current.append(char)
        i += 1
    parts.append("".join(current))
    return [unescape_text(p) for p in parts]


def fold_line(line: str, limit: int = 75) -> str:
    """Fold a content line at ``limit`` octets (RFC 6350 section 3.2).

    Continuation lines start with one space, which counts towards the limit.
    Folds happen on UTF-8 character boundaries. The result has no trailing CRLF.
    """
    if len(line.encode("utf-8")) <= limit:
        return line
    chunks, current, size = [], "", 0
    for char in line:
        width = len(char.encode("utf-8"))
        if size + width > limit:
            chunks.append(current)
            current, size = " ", 1
        current += char
        size += width
    chunks.append(current)
    return "\r\n".join(chunks)
