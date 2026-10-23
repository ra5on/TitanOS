"""Continue TitanOS chats through Assist; execution remains on the NAS."""
import asyncio
from typing import Literal
from uuid import uuid4

from homeassistant.components import conversation
from homeassistant.const import MATCH_ALL
from homeassistant.core import callback
from homeassistant.helpers import intent

async def async_setup_entry(hass, entry, async_add_entities):
    async_add_entities([TitanOSConversation(entry)])

class TitanOSConversation(conversation.ConversationEntity):
    _attr_name = "TitanOS"
    _attr_supported_features = conversation.ConversationEntityFeature.CONTROL

    def __init__(self, entry):
        self._nas_id = entry.data["nas_id"]
        self._attr_unique_id = entry.entry_id

    @property
    def supported_languages(self) -> list[str] | Literal["*"]:
        return MATCH_ALL

    async def _async_handle_message(self, user_input, chat_log):
        request_id = uuid4().hex
        conversation_id = user_input.conversation_id or uuid4().hex
        future = asyncio.get_running_loop().create_future()

        @callback
        def receive(event):
            data = event.data
            if data.get("id") == request_id and data.get("nas_id") == self._nas_id and not future.done():
                future.set_result(str(data.get("text", "")))

        # Subscribe before sending, so even an immediate reply cannot be lost.
        unsubscribe = self.hass.bus.async_listen("titanos_answer", receive)
        try:
            self.hass.bus.async_fire("titanos_ask", {
                "id": request_id, "text": user_input.text,
                "conversation_id": conversation_id, "nas_id": self._nas_id,
            }, context=user_input.context)
            async with asyncio.timeout(65):
                answer = await future
        except TimeoutError:
            answer = ("TitanOS hat noch nicht geantwortet. Prüfe den Auftrag im NAS."
                      if user_input.language.startswith("de") else
                      "TitanOS has not answered yet. Check the task on the NAS.")
        finally:
            unsubscribe()
        # TitanOS owns tool execution; never run model tool results in HA here.
        chat_log.async_add_assistant_content_without_tools(
            conversation.AssistantContent(agent_id=user_input.agent_id, content=answer)
        )
        response = intent.IntentResponse(language=user_input.language)
        response.async_set_speech(answer)
        return conversation.ConversationResult(
            response=response, conversation_id=conversation_id,
            continue_conversation=answer.rstrip().endswith("?"),
        )
