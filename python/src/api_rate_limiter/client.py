import grpc
from check.v1 import check_pb2
from check.v1 import check_pb2_grpc


def dial(addr: str):
    channel = grpc.insecure_channel(addr)
    return channel, check_pb2_grpc.CheckerStub(channel)


def dial_aio(addr: str):
    channel = grpc.aio.insecure_channel(addr)
    return channel, check_pb2_grpc.CheckerStub(channel)


def make_request(key: str, cost: int):
    return check_pb2.CheckRequest(key=key, cost=cost)
