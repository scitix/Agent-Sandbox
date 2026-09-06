"""
Every resource, registered by importing it.

Import order is the only ordering guarantee; nothing here may depend on another
kind being registered first.
"""

from agentbox_sdk.cli.kinds import (  # noqa: F401
    cluster,
    env,
    instancetype,
    pool,
    quota,
    sandbox,
    template,
)
