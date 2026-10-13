`storage-latency-before-aggregation.yaml` is the unmodified deployment manifest
from commit `e3f3ad2`, before the aggregation change. It is a frozen replay
reference, not a manifest to deploy.

The legacy test generator combines its alert definitions with the current
production diagnostic recording rules and deployed definitions of unchanged alerts so the existing threshold/count tests
continue exercising those records. The aggregation test generator separately
tests the production alert definitions and qualification/suppression stages.
