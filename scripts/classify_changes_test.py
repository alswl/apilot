import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('classify', Path(__file__).with_name('classify-changes.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class ClassificationTest(unittest.TestCase):
    def test_documentation(self):
        self.assertFalse(module.requires_full(['README.md', 'docs/architecture.md', 'standards/guides/a.md'], 'pull_request', True))
    def test_code_configuration_and_unknown(self):
        for path in ['internal/ui/forms.go', 'internal/app/service.go', 'Makefile', 'hack/project.mk', 'scripts/check-docs.py', '.github/workflows/build.yml', '.golangci.yml', 'new directory/file.md', 'docs/run.sh']:
            with self.subTest(path=path):
                self.assertTrue(module.requires_full([path], 'pull_request', True))
    def test_tag_dispatch_and_missing_base(self):
        for event in ['tag', 'workflow_dispatch']:
            self.assertTrue(module.requires_full(['README.md'], event, True))
        self.assertTrue(module.requires_full(['README.md'], 'push', False))
        self.assertTrue(module.requires_full([], 'push', True))
    def test_rename_and_deleted_paths(self):
        # git --no-renames yields both the deleted old path and the added new one.
        self.assertTrue(module.requires_full(['internal/old.go', 'docs/old.md'], 'pull_request', True))
        self.assertFalse(module.requires_full(['docs/old name.md', 'docs/new name.md'], 'pull_request', True))

if __name__ == '__main__':
    unittest.main()
