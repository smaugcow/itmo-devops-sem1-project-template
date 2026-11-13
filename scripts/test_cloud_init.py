import unittest

from check_cloud_init import KNOWN_WARNING, ready


class CloudInitTests(unittest.TestCase):
    def test_completed(self):
        self.assertTrue(ready({'status': 'done', 'errors': []}, 0))

    def test_known_warning(self):
        self.assertTrue(ready({'status': 'done', 'recoverable_errors': {'WARNING': [KNOWN_WARNING]}}, 2))

    def test_unknown_warning(self):
        self.assertFalse(ready({'status': 'done', 'recoverable_errors': {'WARNING': ['network setup failed']}}, 2))

    def test_stage_error(self):
        self.assertFalse(ready({'status': 'done', 'modules-final': {'errors': ['failed']}}, 2))

    def test_incomplete_or_fatal(self):
        self.assertFalse(ready({'status': 'running'}, 0))
        self.assertFalse(ready({'status': 'done'}, 1))
        self.assertFalse(ready({'status': 'done'}, 255))


if __name__ == '__main__':
    unittest.main()
