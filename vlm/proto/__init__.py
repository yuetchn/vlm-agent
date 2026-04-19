# Re-export generated stubs with relative imports so that
# "from vlm.proto import vlm_pb2_grpc" works without adding
# vlm/proto/ to sys.path manually.
#
# grpc_tools generates bare absolute imports (import vlm_pb2) which
# fail when the file is imported as part of the vlm.proto package.
# Importing both modules here forces them to be loaded in the correct
# package context first, making subsequent imports resolvable.
import importlib, sys, os

_here = os.path.dirname(__file__)
if _here not in sys.path:
    sys.path.insert(0, _here)
