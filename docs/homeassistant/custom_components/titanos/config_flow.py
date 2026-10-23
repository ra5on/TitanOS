"""Configure a NAS identity, without copying NAS secrets to Home Assistant."""
import re
import voluptuous as vol
from homeassistant import config_entries

class TitanOSConfigFlow(config_entries.ConfigFlow, domain="titanos"):
    VERSION = 1

    async def async_step_user(self, user_input=None):
        errors = {}
        if user_input is not None:
            nas_id = user_input["nas_id"].strip()
            if not re.fullmatch(r"[a-z0-9]{4,32}", nas_id):
                errors["nas_id"] = "invalid_id"
            else:
                await self.async_set_unique_id(nas_id)
                self._abort_if_unique_id_configured()
                return self.async_create_entry(title="TitanOS", data={"nas_id": nas_id})
        return self.async_show_form(step_id="user", data_schema=vol.Schema({vol.Required("nas_id"): str}), errors=errors)
