import sys
from pathlib import Path
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
from smoke import Page, local, totp, CompletedRedirect, RecoveryBrowser, recovery_retry_delay

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

    def test_callback_never_logs_code_or_state(self):
        completed=CompletedRedirect('https://app.vps.example.test/?code=SECRET_CODE&state=SECRET_STATE')
        self.assertEqual(completed.code,'SECRET_CODE')
        self.assertNotIn('SECRET_CODE',str(completed))
        self.assertNotIn('SECRET_STATE',repr(completed))
    def test_callback_refuses_error_duplicate_or_missing_code_state(self):
        for query in ['code=x','state=y','code=x&state=y&code=z','code=x&state=y&state=z','code=x&state=y&error=denied']:
            with self.assertRaises(RuntimeError):
                CompletedRedirect('https://app.vps.example.test/?'+query)
    def test_callback_refuses_foreign_origin_or_path(self):
        for target in ['http://app.vps.example.test/?code=x&state=y','https://app.vps.example.test/other?code=x&state=y','https://outside.example/?code=x&state=y']:
            with self.assertRaises(RuntimeError):
                CompletedRedirect(target)
    def test_pkce_challenge_matches_verifier_and_exchange_checks_state(self):
        import base64,hashlib
        browser=RecoveryBrowser('agrigreen','agrigreen-web-ops')
        self.assertEqual(browser.challenge,base64.urlsafe_b64encode(hashlib.sha256(browser.verifier.encode()).digest()).decode().rstrip('='))
        completed=CompletedRedirect('https://app.vps.example.test/?code=opaque&state=wrong')
        with patch('smoke.call') as request:
            with self.assertRaises(RuntimeError):
                browser.exchange(completed)
            request.assert_not_called()
    def test_negative_otp_probes_respect_quick_login_window(self):
        self.assertGreater(recovery_retry_delay({'quickLoginCheckMilliSeconds':1000}),1)
        self.assertGreater(recovery_retry_delay({'quickLoginCheckMilliSeconds':1}),0.001)
        self.assertGreater(recovery_retry_delay({'quickLoginCheckMilliSeconds':0}),0)
    def test_exchange_uses_actual_code_and_matching_pkce(self):
        browser=RecoveryBrowser('agrigreen','agrigreen-web-ops')
        completed=CompletedRedirect('https://app.vps.example.test/?code=opaque&state='+browser.state)
        with patch('smoke.call',return_value=(200,{'access_token':'opaque-token'})) as request:
            self.assertEqual(browser.exchange(completed),{'access_token':'opaque-token'})
            fields=request.call_args.args[2]
            self.assertEqual(fields['grant_type'],'authorization_code')
            self.assertEqual(fields['code_verifier'],browser.verifier)
            self.assertEqual(fields['code'],'opaque')

if __name__=='__main__':
    unittest.main()
