// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Grpc.Core;
using Paymetheus.Decred;
using Paymetheus.Decred.Wallet;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.Threading.Tasks.Dataflow;
using Walletrpc;
using static Paymetheus.Rpc.Marshalers;

namespace Paymetheus.Rpc
{
    sealed class TransactionNotifications
    {
        public TransactionNotifications(Channel channel, CancellationToken cancelToken)
        {
            _client = WalletService.NewClient(channel);
            _buffer = new BufferBlock<WalletChanges>();
            _cancelToken = cancelToken;
        }

        private readonly WalletService.IWalletServiceClient _client;
        private readonly CancellationToken _cancelToken;
        private readonly BufferBlock<WalletChanges> _buffer;

        public IReceivableSourceBlock<WalletChanges> Buffer => _buffer;

        public async Task ListenAndBuffer()
        {
            const int sec = 1000; // Delays counted in ms

            try
            {
                var request = new TransactionNotificationsRequest();
                using (var stream = _client.TransactionNotifications(request, cancellationToken: _cancelToken))
                {
                    var responses = stream.ResponseStream;

                    while (!_cancelToken.IsCancellationRequested)
                    {
                        var respTask = responses.MoveNext();

                        // If no notifications are received in the next minute, use a ping
                        // (with a 3s pong timeout) to check that connection is still active.
                        while (respTask != await Task.WhenAny(respTask, Task.Delay(60 * sec)))
                        {
                            var pingCall = _client.PingAsync(new PingRequest());
                            var pingTask = pingCall.ResponseAsync;

                            var finishedTask = await Task.WhenAny(respTask, pingTask, Task.Delay(3 * sec));
                            if (finishedTask == respTask)
                                break;
                            else if (finishedTask == pingTask)
                                continue;
                            else
                                throw new TimeoutException("Timeout listening for transaction notification");
                        }

                        // Check whether the stream was ended cleany by the server.  Accessing this
                        // result will throw a RpcException with the Unavailable status code if
                        // the task instead ended due to losing the connection.
                        if (!respTask.Result)
                            break;

                        // Marshal the notification and append to the buffer.
                        var n = responses.Current;
                        var detachedBlocks = n.DetachedBlocks.Select(hash => new Blake256Hash(hash.ToByteArray())).ToHashSet();
                        var attachedBlocks = n.AttachedBlocks.Select(MarshalBlock).ToList();
                        var unminedTransactions = n.UnminedTransactions.Select(MarshalWalletTransaction).ToList();
                        var unminedHashes = n.UnminedTransactionHashes.Select(hash => new Blake256Hash(hash.ToByteArray())).ToHashSet();

                        var changes = new WalletChanges(detachedBlocks, attachedBlocks, unminedTransactions, unminedHashes);
                        _buffer.Post(changes);
                    }
                }

                _buffer.Complete();
            }
            catch (TaskCanceledException)
            {
                _buffer.Complete();
            }
            catch (RpcException ex) when (ex.Status.StatusCode == StatusCode.Cancelled)
            {
                _buffer.Complete();
            }
            catch (Exception ex)
            {
                var block = (IDataflowBlock)_buffer;
                block.Fault(ex);
            }
        }
    }
}
