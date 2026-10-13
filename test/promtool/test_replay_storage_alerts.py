#!/usr/bin/env python3
"""Regression tests for capture ingestion and fail-closed promtool parsing."""
import gzip
import importlib.util
from pathlib import Path
import tempfile
import subprocess
import sys
import unittest

spec = importlib.util.spec_from_file_location('replay', Path(__file__).resolve().parents[2] / 'hack/replay-storage-alerts.py')
replay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(replay)


class ReplayTests(unittest.TestCase):
    def test_timestamp_units(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'metrics.gz'
            for timestamp, unit in [('1700000000.000','seconds'), ('1700000000.125','seconds'), ('1700000000000','milliseconds')]:
                with gzip.open(path, 'wt') as stream:
                    stream.write('kubevirt_vmi_storage_guest_latency_avg_seconds{name="vm"} 0.2 ' + timestamp + '\n')
                values = replay.load([path], 1700000000, 3, unit)
                self.assertEqual(len(values), 1)
                self.assertEqual(next(iter(values.values()))[0], 0.2)

    def test_uncompressed_capture(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'metrics.om'
            path.write_text('kubevirt_vmi_storage_guest_latency_avg_seconds{name="vm"} 0.2 1700000000.000\n# EOF\n')
            self.assertEqual(len(replay.load([path], 1700000000, 3)), 1)

    def test_empty_window_fails_cli(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'metrics.gz'
            with gzip.open(path, 'wt') as stream:
                stream.write('kubevirt_vmi_storage_guest_latency_avg_seconds{name="vm"} 0.2 1700000000\n')
            result = subprocess.run([sys.executable, str(Path(replay.__file__)), '--capture', str(path),
                '--timestamp-unit', 'milliseconds', '--start', '1700000000', '--hours', '1',
                '--output', str(Path(directory) / 'out')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn('no usable samples', result.stderr)

    def test_quoted_labels_and_error_word_are_data(self):
        output = r'''  FAILED:
    expr: "ALERTS{alertstate=\"firing\"}", time: 10m,
        exp: nil
        got: {__name__="ALERTS", alertname="VMIStorageLatencyHigh", name="Error}Injection\"vm", operation="write"} 1E+00
'''
        result = replay.parse_results(output, 0)
        self.assertEqual(result[600][0]['name'], 'Error}Injection"vm')
        self.assertEqual(result[600][0]['operation'], 'write')

    def test_evaluation_error_mixed_with_results_is_rejected(self):
        for output in ['FAILED:\nerror evaluating query: timeout', 'FAILED:\nexpr: "ALERTS", time: 10m,\nexp: nil', 'SUCCESS\nunexpected message']:
            with self.assertRaises(ValueError):
                replay.parse_results(output, 0)

    def test_malformed_vector_is_rejected(self):
        for value in ['{alertname="x"} 1', '{__name__="ALERTS",alertname="x",broken} 1', '{__name__="ALERTS",alertname="x"} 1 garbage']:
            with self.assertRaises(ValueError):
                replay.parse_vector(value)

    def test_success_with_no_alerts(self):
        self.assertEqual(replay.parse_results('  SUCCESS\n', 0), {})


if __name__ == '__main__':
    unittest.main()
