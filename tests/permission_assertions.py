"""Cross-platform assertions for owner-only filesystem permissions."""

import os
import stat
import sys
from pathlib import Path

if sys.platform == "win32":
    import ntsecuritycon
    import win32api
    import win32con
    import win32security


def assert_owner_only_permissions(path: Path, expected_posix_mode: int) -> None:
    """Assert a path is accessible only to the current user."""
    if os.name != "nt":
        mode = stat.S_IMODE(path.stat().st_mode)
        assert mode == expected_posix_mode, f"Expected {oct(expected_posix_mode)}, got {oct(mode)}"
        return

    security_descriptor = win32security.GetNamedSecurityInfo(
        str(path),
        win32security.SE_FILE_OBJECT,
        win32security.OWNER_SECURITY_INFORMATION | win32security.DACL_SECURITY_INFORMATION,
    )
    dacl = security_descriptor.GetSecurityDescriptorDacl()
    control, _revision = security_descriptor.GetSecurityDescriptorControl()

    process_token = win32security.OpenProcessToken(
        win32api.GetCurrentProcess(),
        win32con.TOKEN_QUERY,
    )
    try:
        current_user_sid = win32security.GetTokenInformation(
            process_token,
            win32security.TokenUser,
        )[0]
    finally:
        win32api.CloseHandle(process_token)

    assert control & win32security.SE_DACL_PROTECTED
    owner_sid = security_descriptor.GetSecurityDescriptorOwner()
    assert win32security.ConvertSidToStringSid(owner_sid) == win32security.ConvertSidToStringSid(
        current_user_sid
    )
    assert dacl is not None
    assert dacl.GetAceCount() == 1
    (ace_type, ace_flags), access_mask, sid = dacl.GetAce(0)
    assert ace_type == win32security.ACCESS_ALLOWED_ACE_TYPE
    assert not ace_flags & win32security.INHERITED_ACE
    assert access_mask & ntsecuritycon.FILE_ALL_ACCESS == ntsecuritycon.FILE_ALL_ACCESS
    assert win32security.ConvertSidToStringSid(sid) == win32security.ConvertSidToStringSid(
        current_user_sid
    )
