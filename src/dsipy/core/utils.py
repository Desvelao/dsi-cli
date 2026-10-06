import unicodedata
import re


def slugify(text):
    # Normalize accents (á → a, ñ → n, ü → u)
    normalized = unicodedata.normalize("NFKD", text)
    ascii_text = normalized.encode("ascii", "ignore").decode("ascii")

    # Lowercase
    ascii_text = ascii_text.lower()

    # Replace any non‑alphanumeric group with a single hyphen
    ascii_text = re.sub(r"[^a-z0-9]+", "-", ascii_text)

    # Remove leading/trailing hyphens
    ascii_text = ascii_text.strip("-")

    return ascii_text
