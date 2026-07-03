"""Root-level plugin shim for AgentPaaS.

This module exists so that ``hermes plugins install`` recognizes the repo root
as a valid plugin. Without it, Hermes clones the repo, finds no ``plugin.yaml``
at root, and says "not recognized as a standard Hermes plugin" — the real
plugin lives in the ``integrations/hermes-plugin/`` subdirectory.

When Hermes clones this repo and calls ``register(ctx)``, this shim loads the
real plugin via a virtual package wrapper so its relative imports
(``from . import schemas, tools``) resolve correctly.
"""

import importlib.util
import logging
import sys
import types
from pathlib import Path

logger = logging.getLogger(__name__)

_REAL_PLUGIN_DIR = Path(__file__).resolve().parent / "integrations" / "hermes-plugin"
_PKG_NAME = "agentpaas_plugin_pkg"  # virtual package name for import resolution


def register(ctx):
    """Delegate registration to the real plugin in integrations/hermes-plugin/."""
    if not _REAL_PLUGIN_DIR.is_dir():
        logger.error(
            "AgentPaaS root shim: real plugin directory not found at %s",
            _REAL_PLUGIN_DIR,
        )
        return

    # Create a virtual package so the real plugin's relative imports
    # (from . import schemas, tools) resolve. The real plugin directory
    # has a hyphen in its name ("hermes-plugin") which is not a valid
    # Python module name, so we use a synthetic package name instead.
    pkg = types.ModuleType(_PKG_NAME)
    pkg.__path__ = [str(_REAL_PLUGIN_DIR)]
    sys.modules[_PKG_NAME] = pkg

    spec = importlib.util.spec_from_file_location(
        _PKG_NAME,
        str(_REAL_PLUGIN_DIR / "__init__.py"),
        submodule_search_locations=[str(_REAL_PLUGIN_DIR)],
    )
    if spec is None or spec.loader is None:
        logger.error("AgentPaaS root shim: could not load real plugin spec")
        return

    module = importlib.util.module_from_spec(spec)
    sys.modules[_PKG_NAME] = module
    spec.loader.exec_module(module)

    if hasattr(module, "register"):
        module.register(ctx)
        logger.debug("AgentPaaS root shim: delegated to real plugin successfully")
    else:
        logger.error("AgentPaaS root shim: real plugin has no register() function")
