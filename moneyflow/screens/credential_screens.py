"""Credential setup and unlock screens, quit confirmation, and filter modal."""

from pathlib import Path
from typing import Optional

from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Container
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, Checkbox, Input, Label, Static

from ..credentials import CredentialManager
from ..xero_client import XERO_DEFAULT_SCOPES, build_xero_auth_url


class BackendSelectionScreen(ModalScreen):
    """
    Backend selection screen for first-time setup.

    Keyboard shortcuts:
    - Up/Down: Navigate between backends
    - Enter: Select highlighted backend
    - m: Select Monarch Money
    - y: Select YNAB
    - x: Select Xero
    - Esc: Exit/Cancel
    """

    BINDINGS = [
        Binding("escape", "exit_backend_selector", "Exit", show=False),
        Binding("up", "cursor_up", "Up", show=False),
        Binding("down", "cursor_down", "Down", show=False),
        Binding("enter", "select_current", "Select", show=False),
        Binding("m", "select_monarch", "Monarch", show=False),
        Binding("y", "select_ynab", "YNAB", show=False),
        Binding("x", "select_xero", "Xero", show=False),
    ]

    CSS = """
    BackendSelectionScreen {
        align: center middle;
    }

    #backend-container {
        width: 60;
        height: auto;
        border: solid $accent;
        background: $surface;
        padding: 2 4;
    }

    #backend-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .backend-help {
        color: $text-muted;
        text-align: center;
        margin-bottom: 2;
    }

    .backend-option {
        width: 100%;
        margin: 1 0;
        height: 3;
    }

    .backend-option:focus {
        background: $accent;
        border: thick $accent;
        text-style: bold;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
        margin-top: 2;
    }

    #button-container Button {
        margin: 0 1;
    }
    """

    def __init__(self):
        """Initialize backend selector with tracking for keyboard navigation."""
        super().__init__()
        self.backends = ["monarch", "ynab", "xero"]  # Available backends in order
        self.current_index = 0  # Currently highlighted backend

    def compose(self) -> ComposeResult:
        with Container(id="backend-container"):
            yield Label("💼 Select Finance Backend", id="backend-title")

            yield Static(
                "Choose which personal finance platform you want to connect to.\n"
                "Keys: ↑/↓=Navigate | Enter=Select | m=Monarch | y=YNAB | x=Xero | Esc=Cancel",
                classes="backend-help",
            )

            yield Button(
                "🏦 Monarch Money", variant="default", id="monarch-button", classes="backend-option"
            )

            yield Button("💰 YNAB", variant="default", id="ynab-button", classes="backend-option")

            yield Button("📘 Xero", variant="default", id="xero-button", classes="backend-option")

            with Container(id="button-container"):
                yield Button("Cancel", variant="default", id="exit-button")

    def on_mount(self) -> None:
        """Focus first backend button on load."""
        try:
            button = self.query_one("#monarch-button", Button)
            button.focus()
        except Exception:
            pass

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "exit-button":
            self.dismiss(None)  # Return None instead of exiting app
            return

        if event.button.id == "monarch-button":
            self.dismiss("monarch")

        if event.button.id == "ynab-button":
            self.dismiss("ynab")

        if event.button.id == "xero-button":
            self.dismiss("xero")

    def action_exit_backend_selector(self) -> None:
        """Exit backend selector (Esc key)."""
        self.dismiss(None)

    def action_cursor_up(self) -> None:
        """Move selection up (↑ key)."""
        self.current_index = (self.current_index - 1) % len(self.backends)
        self._focus_current_backend()

    def action_cursor_down(self) -> None:
        """Move selection down (↓ key)."""
        self.current_index = (self.current_index + 1) % len(self.backends)
        self._focus_current_backend()

    def action_select_current(self) -> None:
        """Select currently highlighted backend (Enter key)."""
        backend = self.backends[self.current_index]
        self.dismiss(backend)

    def action_select_monarch(self) -> None:
        """Select Monarch Money (m key)."""
        self.dismiss("monarch")

    def action_select_ynab(self) -> None:
        """Select YNAB (y key)."""
        self.dismiss("ynab")

    def action_select_xero(self) -> None:
        """Select Xero (x key)."""
        self.dismiss("xero")

    def _focus_current_backend(self) -> None:
        """Focus the button for currently selected backend."""
        backend = self.backends[self.current_index]
        button_id = f"{backend}-button"
        try:
            button = self.query_one(f"#{button_id}", Button)
            button.focus()
        except Exception:
            pass


class CredentialSetupScreen(ModalScreen):
    """First-time credential setup screen."""

    CSS = """
    CredentialSetupScreen {
        align: center middle;
    }

    #setup-container {
        width: 70;
        height: auto;
        border: solid $accent;
        background: $surface;
        padding: 2 4;
    }

    #setup-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .setup-label {
        margin-top: 1;
        color: $text;
    }

    .setup-input {
        margin-bottom: 1;
    }

    .setup-help {
        color: $text-muted;
        text-style: italic;
        margin-bottom: 1;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
        margin-top: 2;
    }

    #button-container Button {
        margin: 0 1;
    }

    #error-label {
        color: $error;
        text-align: center;
        margin-top: 1;
    }
    """

    def __init__(self, backend_type: str = "monarch", profile_dir: Optional[Path] = None):
        """
        Initialize credential setup screen.

        Args:
            backend_type: Backend type (monarch, ynab, etc.)
            profile_dir: Optional profile directory for multi-account mode
                        If provided, credentials will be saved in profile_dir
        """
        super().__init__()
        self.backend_type = backend_type
        self.profile_dir = profile_dir
        self._xero_auth_url: str = ""

    def compose(self) -> ComposeResult:
        with Container(id="setup-container"):
            if self.backend_type == "ynab":
                yield Label("🔐 YNAB Credential Setup", id="setup-title")

                yield Static(
                    "This will securely store your YNAB Personal Access Token\n"
                    "encrypted with a password of your choice.",
                    classes="setup-help",
                )

                yield Label("YNAB Personal Access Token:", classes="setup-label")
                yield Static(
                    "Get this from: Account Settings → Developer Settings → New Token",
                    classes="setup-help",
                )
                yield Input(
                    placeholder="your access token",
                    password=True,
                    id="password-input",
                    classes="setup-input",
                )
            elif self.backend_type == "xero":
                yield Label("🔐 Xero Credential Setup", id="setup-title")

                yield Static(
                    "Enter your Xero OAuth app credentials. We'll store them encrypted per profile\n"
                    "and use them to refresh tokens automatically.",
                    classes="setup-help",
                )

                yield Label("Xero Client ID:", classes="setup-label")
                yield Input(
                    placeholder="client id from Xero app",
                    id="xero-client-id-input",
                    classes="setup-input",
                )

                yield Label("Xero Client Secret:", classes="setup-label")
                yield Input(
                    placeholder="client secret",
                    password=True,
                    id="xero-client-secret-input",
                    classes="setup-input",
                )

                yield Label("OAuth Redirect URI:", classes="setup-label")
                yield Input(
                    placeholder="https://your-app/callback",
                    id="xero-redirect-uri-input",
                    classes="setup-input",
                )

                yield Label("Initial Refresh Token:", classes="setup-label")
                yield Static(
                    "Open the auth URL, complete consent, exchange the code for a refresh token,\n"
                    "and paste the refresh token here. We'll keep it encrypted and refresh it.",
                    classes="setup-help",
                )
                yield Input(
                    placeholder="refresh token",
                    password=True,
                    id="xero-refresh-token-input",
                    classes="setup-input",
                )

                yield Static(
                    f"Scopes auto-set: {XERO_DEFAULT_SCOPES}",
                    classes="setup-help",
                )

                yield Button("Generate auth URL", variant="default", id="xero-generate-url-button")
                yield Label("Auth URL (focus then Cmd/Ctrl+C to copy):", classes="setup-label")
                yield Input(
                    placeholder="generate the URL above",
                    id="xero-auth-url-input",
                    classes="setup-input",
                )
                yield Button("Copy URL to clipboard", variant="default", id="xero-copy-url-button")
                yield Button("Open URL in browser", variant="default", id="xero-open-url-button")
                yield Static("", id="xero-auth-url-status", classes="setup-help")
            else:
                yield Label("🔐 Monarch Money Credential Setup", id="setup-title")

                yield Static(
                    "This will securely store your Monarch Money credentials\n"
                    "encrypted with a password of your choice.",
                    classes="setup-help",
                )

                yield Label("Monarch Money Email:", classes="setup-label")
                yield Input(placeholder="your@email.com", id="email-input", classes="setup-input")

                yield Label("Monarch Money Password:", classes="setup-label")
                yield Input(
                    placeholder="password",
                    password=True,
                    id="password-input",
                    classes="setup-input",
                )

                yield Label("2FA/TOTP Secret Key (~32 characters):", classes="setup-label")
                yield Static(
                    "Get this from: Settings → Security → Re-enable 2FA → 'Can't scan?'\n"
                    "Should be a ~32 character base32 string (not the 6-digit code)",
                    classes="setup-help",
                )
                yield Input(
                    placeholder="JBSWY3DPEHPK3PXP (base32 string)",
                    id="mfa-input",
                    classes="setup-input",
                )

            yield Label("Encryption Password (for moneyflow):", classes="setup-label")
            yield Static(
                "Create a NEW password to encrypt your stored credentials", classes="setup-help"
            )
            yield Input(
                placeholder="encryption password",
                password=True,
                id="encrypt-pass-input",
                classes="setup-input",
            )

            yield Label("Confirm Encryption Password:", classes="setup-label")
            yield Input(
                placeholder="confirm password",
                password=True,
                id="confirm-pass-input",
                classes="setup-input",
            )

            with Container(id="button-container"):
                yield Button("Save Credentials", variant="primary", id="save-button")
                yield Button("Exit", variant="default", id="exit-button")

            yield Label("", id="error-label")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "exit-button":
            self.app.exit()
            return

        if event.button.id == "xero-generate-url-button":
            self._show_xero_auth_url()
            return

        if event.button.id == "xero-copy-url-button":
            self._copy_xero_auth_url()
            return

        if event.button.id == "xero-open-url-button":
            self._open_xero_auth_url()
            return

        if event.button.id == "save-button":
            await self.save_credentials()

    async def save_credentials(self) -> None:
        """Validate and save credentials."""
        error_label = self.query_one("#error-label", Label)

        encrypt_pass = self.query_one("#encrypt-pass-input", Input).value
        confirm_pass = self.query_one("#confirm-pass-input", Input).value
        extra_credentials: dict[str, str] = {}

        if encrypt_pass != confirm_pass:
            error_label.update("❌ Encryption passwords do not match!")
            return

        if self.backend_type == "ynab":
            password = self.query_one("#password-input", Input).value.strip()

            if not password or not encrypt_pass:
                error_label.update("❌ Please fill in all fields")
                return

            email = ""
            mfa_secret = ""
        elif self.backend_type == "xero":
            client_id = self.query_one("#xero-client-id-input", Input).value.strip()
            client_secret = self.query_one("#xero-client-secret-input", Input).value.strip()
            redirect_uri = self.query_one("#xero-redirect-uri-input", Input).value.strip()
            refresh_token = self.query_one("#xero-refresh-token-input", Input).value.strip()

            if (
                not client_id
                or not client_secret
                or not redirect_uri
                or not refresh_token
                or not encrypt_pass
            ):
                error_label.update("❌ Please fill in all fields")
                return

            email = ""
            password = ""
            mfa_secret = ""
            extra_credentials = {
                "client_id": client_id,
                "client_secret": client_secret,
                "redirect_uri": redirect_uri,
                "refresh_token": refresh_token,
                "scopes": XERO_DEFAULT_SCOPES,
            }
        else:
            email = self.query_one("#email-input", Input).value.strip()
            password = self.query_one("#password-input", Input).value
            mfa_secret = self.query_one("#mfa-input", Input).value.strip().replace(" ", "").upper()

            if not email or not password or not mfa_secret or not encrypt_pass:
                error_label.update("❌ Please fill in all fields")
                return

            if "@" not in email:
                error_label.update("❌ Invalid email address")
                return

        # Save credentials
        try:
            error_label.update("💾 Saving credentials...")
            # Get config_dir from app and pass to CredentialManager
            config_path = Path(self.app.config_dir) if self.app.config_dir else None
            cred_manager = CredentialManager(config_dir=config_path, profile_dir=self.profile_dir)
            cred_manager.save_credentials(
                email=email,
                password=password,
                mfa_secret=mfa_secret,
                encryption_password=encrypt_pass,
                backend_type=self.backend_type,
                extra_credentials=extra_credentials,
            )

            # Load credentials back to get the encryption key for cache encryption
            _, encryption_key = cred_manager.load_credentials(encryption_password=encrypt_pass)
            self.app.encryption_key = encryption_key

            error_label.update("✅ Credentials saved! Loading app...")

            # Dismiss this screen and pass credentials back (including backend type)
            credentials = {
                "email": email,
                "password": password,
                "mfa_secret": mfa_secret,
                "backend_type": self.backend_type,
            }
            credentials.update(extra_credentials)
            self.dismiss(credentials)

        except Exception as e:
            error_label.update(f"❌ Error saving credentials: {e}")

    def _show_xero_auth_url(self) -> None:
        """Build and display the Xero auth URL based on current inputs."""
        auth_input = self.query_one("#xero-auth-url-input", Input)
        client_id = self.query_one("#xero-client-id-input", Input).value.strip()
        redirect_uri = self.query_one("#xero-redirect-uri-input", Input).value.strip()
        scopes = XERO_DEFAULT_SCOPES

        if not client_id or not redirect_uri:
            auth_input.value = "❌ Enter Client ID and Redirect URI to generate the auth URL."
            self._xero_auth_url = ""
            return

        url = build_xero_auth_url(client_id=client_id, redirect_uri=redirect_uri, scopes=scopes)
        auth_input.value = url
        self._xero_auth_url = url
        auth_input.focus()

    def _copy_xero_auth_url(self) -> None:
        """Copy the generated auth URL to the system clipboard if possible."""
        status = self.query_one("#xero-auth-url-status", Static)
        if not self._xero_auth_url:
            status.update("❌ Generate the auth URL first.")
            return

        if self._copy_to_clipboard(self._xero_auth_url):
            status.update("✅ Copied to clipboard.")
        else:
            status.update("⚠ Could not access clipboard. Manually copy from the field above.")

    def _open_xero_auth_url(self) -> None:
        """Open the generated auth URL in the default browser (if available)."""
        status = self.query_one("#xero-auth-url-status", Static)
        if not self._xero_auth_url:
            status.update("❌ Generate the auth URL first.")
            return

        try:
            import webbrowser

            opened = webbrowser.open(self._xero_auth_url)
            if opened:
                status.update("✅ Opened in your default browser.")
            else:
                status.update("⚠ Could not open browser. Copy the URL manually from the field above.")
        except Exception:
            status.update("⚠ Could not open browser. Copy the URL manually from the field above.")

    @staticmethod
    def _copy_to_clipboard(text: str) -> bool:
        """Best-effort copy to clipboard without extra dependencies."""
        import platform
        import shutil
        import subprocess

        system = platform.system().lower()

        try:
            if system == "darwin" and shutil.which("pbcopy"):
                subprocess.run("pbcopy", input=text, text=True, check=True)
                return True
            if system == "windows" and shutil.which("clip"):
                subprocess.run("clip", input=text, text=True, check=True)
                return True
            if system == "linux":
                if shutil.which("xclip"):
                    subprocess.run(["xclip", "-selection", "clipboard"], input=text, text=True, check=True)
                    return True
                if shutil.which("xsel"):
                    subprocess.run(["xsel", "--clipboard", "--input"], input=text, text=True, check=True)
                    return True
        except Exception:
            return False

        return False


class CredentialUnlockScreen(ModalScreen):
    """Screen to unlock encrypted credentials."""

    def __init__(self, profile_dir: Optional[Path] = None):
        """
        Initialize credential unlock screen.

        Args:
            profile_dir: Optional profile directory for multi-account mode
        """
        super().__init__()
        self.profile_dir = profile_dir

    CSS = """
    CredentialUnlockScreen {
        align: center middle;
    }

    #unlock-container {
        width: 60;
        height: auto;
        border: solid $accent;
        background: $surface;
        padding: 2 4;
    }

    #unlock-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .unlock-help {
        color: $text-muted;
        text-align: center;
        margin-bottom: 2;
    }

    .unlock-label {
        margin-top: 1;
        color: $text;
    }

    .unlock-input {
        margin-bottom: 1;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
        margin-top: 2;
    }

    #button-container Button {
        margin: 0 1;
    }

    #error-label {
        color: $error;
        text-align: center;
        margin-top: 1;
    }
    """

    def compose(self) -> ComposeResult:
        with Container(id="unlock-container"):
            yield Label("🔓 Unlock Credentials", id="unlock-title")

            yield Static(
                "Enter your encryption password to unlock stored credentials", classes="unlock-help"
            )

            yield Label("Encryption Password:", classes="unlock-label")
            yield Input(
                placeholder="encryption password",
                password=True,
                id="unlock-input",
                classes="unlock-input",
            )

            with Container(id="button-container"):
                yield Button("Unlock", variant="primary", id="unlock-button")
                yield Button("Reset Credentials", variant="warning", id="reset-button")
                yield Button("Exit", variant="default", id="exit-button")

            yield Label("", id="error-label")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "exit-button":
            self.app.exit()
            return

        if event.button.id == "reset-button":
            await self.reset_credentials()
            return

        if event.button.id == "unlock-button":
            await self.unlock_credentials()

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle Enter key in password input."""
        await self.unlock_credentials()

    async def unlock_credentials(self) -> None:
        """Try to unlock credentials with provided password."""
        error_label = self.query_one("#error-label", Label)
        unlock_input = self.query_one("#unlock-input", Input)

        encryption_password = unlock_input.value

        if not encryption_password:
            error_label.update("❌ Please enter password")
            return

        try:
            error_label.update("🔓 Unlocking...")
            # Get config_dir from app and pass to CredentialManager
            config_path = Path(self.app.config_dir) if self.app.config_dir else None
            cred_manager = CredentialManager(config_dir=config_path, profile_dir=self.profile_dir)
            creds, encryption_key = cred_manager.load_credentials(
                encryption_password=encryption_password
            )

            error_label.update("✅ Unlocked! Logging in...")

            # Store encryption key in app for cache encryption
            self.app.encryption_key = encryption_key

            # Dismiss and return credentials
            self.dismiss(creds)

        except ValueError:
            error_label.update("❌ Incorrect password!")
            unlock_input.value = ""
            unlock_input.focus()
        except Exception as e:
            error_label.update(f"❌ Error: {e}")

    async def reset_credentials(self) -> None:
        """Delete credentials and show setup screen."""
        try:
            # Get config_dir from app and pass to CredentialManager
            config_path = Path(self.app.config_dir) if self.app.config_dir else None
            cred_manager = CredentialManager(config_dir=config_path, profile_dir=self.profile_dir)
            cred_manager.delete_credentials()

            # Switch to setup screen
            self.dismiss(None)  # Signal to show setup screen

        except Exception as e:
            error_label = self.query_one("#error-label", Label)
            error_label.update(f"❌ Error resetting: {e}")


class QuitConfirmationScreen(ModalScreen):
    """Confirmation screen before quitting."""

    CSS = """
    QuitConfirmationScreen {
        align: center middle;
    }

    #quit-dialog {
        width: 50;
        height: auto;
        border: thick $warning;
        background: $surface;
        padding: 2 4;
    }

    #quit-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $warning;
        margin-bottom: 1;
    }

    #quit-message {
        text-align: center;
        color: $text;
        margin-bottom: 2;
    }

    #quit-instructions {
        text-align: center;
        color: $accent;
        margin-bottom: 2;
        text-style: bold;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
    }

    #button-container Button {
        margin: 0 1;
    }
    """

    def __init__(self, has_unsaved_changes: bool = False):
        super().__init__()
        self.has_unsaved_changes = has_unsaved_changes

    def compose(self) -> ComposeResult:
        with Container(id="quit-dialog"):
            yield Label("⚠️  Quit moneyflow?", id="quit-title")

            if self.has_unsaved_changes:
                yield Static(
                    "You have unsaved changes!\nThey will be lost if you quit now.",
                    id="quit-message",
                )
            else:
                yield Static("Are you sure you want to quit?", id="quit-message")

            yield Static("y=Quit | n/Esc=Cancel", id="quit-instructions")

            with Container(id="button-container"):
                yield Button("Cancel (N)", variant="primary", id="cancel-button")
                yield Button("Quit (Y)", variant="error", id="quit-button")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(False)
        elif event.button.id == "quit-button":
            self.dismiss(True)

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts - Y to quit, N or Esc to cancel."""
        if event.key in ("escape", "n"):
            self.dismiss(False)
        elif event.key in ("y", "q"):
            self.dismiss(True)


class FilterScreen(ModalScreen):
    """
    Filter options modal with full keyboard navigation.

    Keyboard shortcuts:
    - h: Toggle show hidden transactions
    - t: Toggle show transfers
    - Enter/Space: Apply filters
    - Esc: Cancel
    """

    CSS = """
    FilterScreen {
        align: center middle;
    }

    #filter-dialog {
        width: 50;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #filter-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 2;
    }

    #filter-instructions {
        text-align: center;
        color: $text-muted;
        margin-bottom: 1;
    }

    .filter-option {
        margin: 1 0;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
        margin-top: 2;
    }

    #button-container Button {
        margin: 0 1;
    }
    """

    def __init__(self, show_transfers: bool = False, show_hidden: bool = True):
        super().__init__()
        self.show_transfers = show_transfers
        self.show_hidden = show_hidden

    def compose(self) -> ComposeResult:
        with Container(id="filter-dialog"):
            yield Label("🔍 Filter Options", id="filter-title")

            yield Static(
                "h=Toggle hidden | t=Toggle transfers | Enter=Apply | Esc=Cancel",
                id="filter-instructions",
            )

            yield Checkbox(
                "Show hidden from reports transactions (H)",
                value=self.show_hidden,
                id="show-hidden-checkbox",
                classes="filter-option",
            )

            yield Checkbox(
                "Show Transfer transactions (T)",
                value=self.show_transfers,
                id="show-transfers-checkbox",
                classes="filter-option",
            )

            with Container(id="button-container"):
                yield Button("Apply (Enter)", variant="primary", id="apply-button")
                yield Button("Cancel (Esc)", variant="default", id="cancel-button")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(None)
        elif event.button.id == "apply-button":
            # Get checkbox values
            show_hidden = self.query_one("#show-hidden-checkbox", Checkbox).value
            show_transfers = self.query_one("#show-transfers-checkbox", Checkbox).value
            self.dismiss({"show_hidden": show_hidden, "show_transfers": show_transfers})

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts for filter modal."""
        if event.key == "escape":
            event.stop()  # Prevent propagation
            self.dismiss(None)
        elif event.key in ("enter", "space"):
            event.stop()  # Prevent propagation to parent
            # Apply filters
            show_hidden = self.query_one("#show-hidden-checkbox", Checkbox).value
            show_transfers = self.query_one("#show-transfers-checkbox", Checkbox).value
            self.dismiss({"show_hidden": show_hidden, "show_transfers": show_transfers})
        elif event.key == "h":
            event.stop()  # Prevent propagation
            # Toggle hidden checkbox
            checkbox = self.query_one("#show-hidden-checkbox", Checkbox)
            checkbox.value = not checkbox.value
        elif event.key == "t":
            event.stop()  # Prevent propagation
            # Toggle transfers checkbox
            checkbox = self.query_one("#show-transfers-checkbox", Checkbox)
            checkbox.value = not checkbox.value


class CachePromptScreen(ModalScreen):
    """Prompt to use cached data or refresh from API."""

    CSS = """
    CachePromptScreen {
        align: center middle;
    }

    #cache-dialog {
        width: 60;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #cache-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #cache-info {
        text-align: center;
        color: $text;
        margin-bottom: 2;
    }

    #cache-instructions {
        text-align: center;
        color: $text-muted;
        margin-bottom: 2;
        text-style: italic;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
    }

    #button-container Button {
        margin: 0 1;
    }
    """

    def __init__(self, age: str, transaction_count: int, filter_desc: str):
        super().__init__()
        self.age = age
        self.transaction_count = transaction_count
        self.filter_desc = filter_desc

    def compose(self) -> ComposeResult:
        with Container(id="cache-dialog"):
            yield Label("📦 Cached Data Available", id="cache-title")

            cache_message = (
                f"Found cached data from {self.age}\n"
                f"{self.transaction_count:,} transactions ({self.filter_desc})\n\n"
                f"Use cached data for faster load?"
            )
            yield Static(cache_message, id="cache-info")

            yield Static("y=Use cache | n=Refresh | Esc=Cancel", id="cache-instructions")

            with Container(id="button-container"):
                yield Button("Use Cache (Y)", variant="primary", id="cache-button")
                yield Button("Refresh (N)", variant="default", id="refresh-button")

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cache-button":
            self.dismiss(True)  # Use cache
        elif event.button.id == "refresh-button":
            self.dismiss(False)  # Refresh from API

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts - Y to use cache, N to refresh, Esc to cancel."""
        if event.key == "escape":
            # Default to refresh if cancelled
            self.dismiss(False)
        elif event.key == "y":
            self.dismiss(True)
        elif event.key == "n":
            self.dismiss(False)
