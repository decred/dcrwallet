# RPC Documentation

This project provides a [gRPC](https://www.grpc.io/) server for Remote Procedure
Call (RPC) access from other processes.  This is intended to be the primary
means by which users, through other client programs, interact with the wallet.

These documents cover the documentation for both consumers of the server and
developers who must make changes or additions to the API and server
implementation:

- [API specification](./api.md)
- [Client usage](./clientusage.md)
- [Making API changes](./serverchanges.md)

A legacy JSON-RPC server is also available, but documenting its usage
it out of scope for these documents.
