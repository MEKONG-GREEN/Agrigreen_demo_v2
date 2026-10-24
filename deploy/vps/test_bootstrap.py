import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import sys
import threading
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from bootstrap import Bootstrap

class BootstrapTest(unittest.TestCase):
    def setUp(self):
        self.calls=[]
        self.bad_commune=False
        self.reject=False
        owner=self
        class Handler(BaseHTTPRequestHandler):
            def run(self):
                raw=self.rfile.read(int(self.headers.get("Content-Length","0")))
                body=json.loads(raw) if raw else None
                owner.calls.append((self.command,self.path,body))
                assert self.headers["Authorization"]=="Bearer synthetic-test"
                path=self.path.split("?")[0]
                response={}
                if path=="/v1/masterdata/admin-units/91":
                    response={"code":"91","name":"An Giang","level":"province"}
                elif path=="/v1/masterdata/admin-units/30292":
                    response={"code":"30292","name":"Binh Duc","level":"commune","parent_code":"92" if owner.bad_commune else "91"}
                elif path=="/v1/orgs":
                    response={"org":{"ref":"org-"+body["code"]}}
                elif path=="/v1/geo/admin-boundaries/import":
                    response={"accepted":0 if owner.reject else 1,"rejected":1 if owner.reject else 0}
                encoded=json.dumps(response).encode()
                self.send_response(200)
                self.send_header("Content-Length",str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)
            do_GET=run
            do_POST=run
            def log_message(self,*_):
                pass
        self.server=ThreadingHTTPServer(("127.0.0.1",0),Handler)
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True)
        self.thread.start()
        base="http://127.0.0.1:"+str(self.server.server_port)
        self.client=Bootstrap(base,base,"synthetic-test")
        self.fixture={"type":"FeatureCollection","features":[{"properties":{"code":"30292"},"geometry":{"type":"Polygon","coordinates":[]}}]}
    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
    def test_reference_bootstrap_never_writes_htx(self):
        result=self.client.run("91",self.fixture,"Synthetic test")
        self.assertEqual(result["commune_boundaries"],1)
        creates=[b for m,p,b in self.calls if m=="POST" and p=="/v1/orgs"]
        self.assertEqual([b["kind"] for b in creates],["ttkn_national","ttkn_province","extension_team"])
        self.assertEqual(creates[2]["admin_unit_code"],"30292")
        self.assertTrue(all("cooperative" not in p and "/kn/" not in p for _,p,_ in self.calls))
    def test_other_province_stops_before_tree_or_geometry_writes(self):
        self.bad_commune=True
        with self.assertRaisesRegex(ValueError,"does not belong"):
            self.client.run("91",self.fixture,"Synthetic test")
        self.assertFalse(any(p=="/v1/orgs" or "admin-boundaries/import" in p for _,p,_ in self.calls))
    def test_rejected_preview_never_creates_tree(self):
        self.reject=True
        with self.assertRaisesRegex(ValueError,"preview rejected"):
            self.client.run("91",self.fixture,"Synthetic test")
        self.assertFalse(any(p=="/v1/orgs" for _,p,_ in self.calls))
    def test_empty_and_duplicate_codes_are_rejected_before_requests(self):
        self.fixture["features"].append(self.fixture["features"][0])
        with self.assertRaisesRegex(ValueError,"unique"):
            self.client.run("91",self.fixture,"Synthetic test")
        self.assertEqual(self.calls,[])

if __name__=="__main__":
    unittest.main()
