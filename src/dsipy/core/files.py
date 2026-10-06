import os
from pathlib import Path
from typing import List, Optional

allowed_vcard_extensions = [".vcf", ".vcard"]


def file_is_vcard(f):
    if isinstance(f, str):
        return f.lower().endswith(tuple(allowed_vcard_extensions))
    if isinstance(f, Path):
        return f.suffix.lower() in allowed_vcard_extensions
    return False


def get_local_files_from_inputs(
    inputs: List[Path], filter_func, warnings: Optional[List[str]] = None
) -> List[Path]:
    """Collect matching files; problems with inputs are appended to `warnings`."""
    warnings = warnings if warnings is not None else []
    files = []
    for input_path in inputs:
        if not input_path.exists():
            warnings.append(f"Input path does not exist: {input_path}")
            continue
        if not input_path.is_file() and not input_path.is_dir():
            warnings.append(f"Input path is not a file or directory: {input_path}")
            continue
        if input_path.is_file() and filter_func(input_path):
            files.append(input_path)
        if input_path.is_dir():
            for root, _, dir_files in os.walk(input_path):
                for f in dir_files:
                    filepath = Path(root) / f
                    if filter_func(filepath):
                        files.append(filepath)
    return files
