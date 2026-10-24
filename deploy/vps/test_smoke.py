import sys
from pathlib import Path
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
from smoke import Page, local, totp

class SmokeHarnessTest(unittest.TestCase):
    def test_keycloak_relative_recovery_url_keeps_approved_host(self):
        page=Page('<a href="/realms/agrigreen/login-actions/reset-credentials?client_id=x&amp;tab_id=y">forgot</a>')
        self.assertEqual(local(page.reset),'https://auth.vps.example.test/realms/agrigreen/login-actions/reset-credentials?client_id=x&tab_id=y')
    def test_recovery_url_refuses_foreign_host_or_plain_http(self):
        for target in ['//outside.example/reset','https://outside.example/reset','http://auth.vps.example.test/reset']:
            with self.assertRaises(RuntimeError):
                local(target)
    def test_password_form_preserves_hidden_fields(self):
        page=Page('<form action="/realms/agrigreen/login-actions/required-action"><input type="hidden" name="state" value="s"><input name="password-new" type="password"><input name="password-confirm" type="password"></form>')
        self.assertEqual(page.fields,{'state':'s'})
        self.assertTrue({'password-new','password-confirm'}.issubset(page.inputs))
    def test_totp_matches_rfc6238_sha1_vector(self):
        with patch('smoke.time.time',return_value=59):
            self.assertEqual(totp('12345678901234567890'),'287082')

if __name__=='__main__':
    unittest.main()
