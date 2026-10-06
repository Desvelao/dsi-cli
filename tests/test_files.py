import tempfile
import unittest
from pathlib import Path

from src.dsipy.core.files import file_is_vcard, get_local_files_from_inputs


class FileIsVcardTests(unittest.TestCase):
    def test_str_case_insensitive(self):
        for name in ("a.vcf", "a.VCF", "a.vCard", "a.vcard"):
            self.assertIs(file_is_vcard(name), True)

    def test_path_case_insensitive(self):
        for name in ("a.vcf", "a.VCF", "a.VCARD"):
            self.assertIs(file_is_vcard(Path(name)), True)

    def test_non_vcard(self):
        self.assertIs(file_is_vcard("a.txt"), False)
        self.assertIs(file_is_vcard(Path("a.txt")), False)

    def test_other_types_return_false(self):
        for value in (None, 1, b"a.vcf", ["a.vcf"]):
            self.assertIs(file_is_vcard(value), False)


class GetLocalFilesTests(unittest.TestCase):
    def test_missing_path_collected_as_warning(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / "a.VCF").write_text("x")
            (root / "b.txt").write_text("x")
            missing = root / "missing"
            warnings = []
            files = get_local_files_from_inputs(
                [root, missing], file_is_vcard, warnings
            )
            self.assertEqual(files, [root / "a.VCF"])
            self.assertEqual(warnings, [f"Input path does not exist: {missing}"])

    def test_warnings_optional(self):
        self.assertEqual(
            get_local_files_from_inputs([Path("/nonexistent-x")], file_is_vcard), []
        )


if __name__ == "__main__":
    unittest.main()
