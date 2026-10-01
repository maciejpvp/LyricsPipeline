from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class QueueMessage:
    message_id: str
    receipt_handle: str
    body: str
    receive_count: int


class MessageQueue(Protocol):
    def receive(self, wait_seconds: int, visibility_timeout: int) -> list[QueueMessage]: ...
    def delete(self, receipt_handle: str) -> None: ...
    def change_visibility(self, receipt_handle: str, timeout: int) -> None: ...


class SQSQueue:
    def __init__(self, client, queue_url: str):
        self.client = client
        self.queue_url = queue_url

    def receive(self, wait_seconds: int, visibility_timeout: int) -> list[QueueMessage]:
        response = self.client.receive_message(
            QueueUrl=self.queue_url, MaxNumberOfMessages=1, WaitTimeSeconds=wait_seconds,
            VisibilityTimeout=visibility_timeout, AttributeNames=["ApproximateReceiveCount"],
        )
        return [QueueMessage(item["MessageId"], item["ReceiptHandle"], item["Body"],
                             int(item.get("Attributes", {}).get("ApproximateReceiveCount", "1")))
                for item in response.get("Messages", [])]

    def delete(self, receipt_handle: str) -> None:
        self.client.delete_message(QueueUrl=self.queue_url, ReceiptHandle=receipt_handle)

    def change_visibility(self, receipt_handle: str, timeout: int) -> None:
        self.client.change_message_visibility(
            QueueUrl=self.queue_url, ReceiptHandle=receipt_handle, VisibilityTimeout=timeout
        )
