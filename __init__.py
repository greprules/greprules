"""Hermes plugin entry point for the greprules monorepo.

Hermes installs Git repositories as plugin roots. The actual greprules Hermes
adapter lives under plugins/hermes so agent-specific code stays grouped in the
monorepo; this shim delegates registration to that implementation.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from types import ModuleType

_IMPL_MODULE: ModuleType | None = None


def _load_impl() -> ModuleType:
    global _IMPL_MODULE
    if _IMPL_MODULE is not None:
        return _IMPL_MODULE

    impl_dir = Path(__file__).resolve().parent / "plugins" / "hermes"
    init_file = impl_dir / "__init__.py"
    if not init_file.exists():
        raise FileNotFoundError(f"greprules Hermes implementation not found: {init_file}")

    module_name = f"{__name__}._impl"
    spec = importlib.util.spec_from_file_location(
        module_name,
        init_file,
        submodule_search_locations=[str(impl_dir)],
    )
    if spec is None or spec.loader is None:
        raise ImportError(f"Cannot load greprules Hermes implementation from {init_file}")

    module = importlib.util.module_from_spec(spec)
    module.__package__ = module_name
    module.__path__ = [str(impl_dir)]  # type: ignore[attr-defined]
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    _IMPL_MODULE = module
    return module


def register(ctx) -> None:
    _load_impl().register(ctx)
