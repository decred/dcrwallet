// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Google.Protobuf;
using Grpc.Core;
using Paymetheus.Decred;
using Paymetheus.Decred.Script;
using Paymetheus.Decred.Wallet;
using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.Threading.Tasks.Dataflow;
using Walletrpc;
using static Paymetheus.Rpc.Marshalers;

namespace Paymetheus.Rpc
{
    public sealed class WalletClient : IDisposable
    {
        private static readonly SemanticVersion RequiredRpcServerVersion = new SemanticVersion(2, 0, 0);

        public static void Initialize()
        {
            Environment.SetEnvironmentVariable("GRPC_SSL_CIPHER_SUITES", "HIGH+ECDSA");
        }

        private WalletClient(Channel channel)
        {
            _channel = channel;
            _tokenSource = new CancellationTokenSource();
        }

        private readonly Channel _channel;
        private readonly CancellationTokenSource _tokenSource;

        public void Dispose()
        {
            _tokenSource.Dispose();
        }

        public void CancelRequests()
        {
            _tokenSource.Cancel();
        }

        public async Task Disconnect()
        {
            await _channel.ShutdownAsync();
        }

        public static async Task<WalletClient> ConnectAsync(string networkAddress, string rootCertificate)
        {
            if (networkAddress == null)
                throw new ArgumentNullException(nameof(networkAddress));
            if (rootCertificate == null)
                throw new ArgumentNullException(nameof(rootCertificate));

            var channel = new Channel(networkAddress, new SslCredentials(rootCertificate));
            var deadline = DateTime.UtcNow.AddSeconds(3);
            try
            {
                await channel.ConnectAsync(deadline);
            }
            catch (TaskCanceledException)
            {
                await channel.ShutdownAsync();
                throw new ConnectTimeoutException();
            }

            // Ensure the server is running a compatible version.
            var versionClient = VersionService.NewClient(channel);
            var response = await versionClient.VersionAsync(new VersionRequest(), deadline: deadline);
            var serverVersion = new SemanticVersion(response.Major, response.Minor, response.Patch);
            SemanticVersion.AssertCompatible(RequiredRpcServerVersion, serverVersion);

            return new WalletClient(channel);
        }

        public async Task<bool> WalletExistsAsync()
        {
            var client = WalletLoaderService.NewClient(_channel);
            var resp = await client.WalletExistsAsync(new WalletExistsRequest(), cancellationToken: _tokenSource.Token);
            return resp.Exists;
        }

        public async Task StartConsensusRpc(ConsensusServerRpcOptions options)
        {
            var certificateTask = ReadFileAsync(options.CertificatePath);
            var client = WalletLoaderService.NewClient(_channel);
            var request = new StartConsensusRpcRequest
            {
                NetworkAddress = options.NetworkAddress,
                Username = options.RpcUser,
                Password = ByteString.CopyFromUtf8(options.RpcPassword),
                Certificate = ByteString.CopyFrom(await certificateTask),
            };
            await client.StartConsensusRpcAsync(request, cancellationToken: _tokenSource.Token);
        }

        private async Task<byte[]> ReadFileAsync(string filePath)
        {
            using (var stream = new FileStream(filePath, FileMode.Open))
            {
                var fileSize = stream.Length;
                if (fileSize > int.MaxValue)
                    throw new Exception("File is too large to read into array");

                var result = new byte[(int)fileSize];
                await stream.ReadAsync(result, 0, (int)fileSize);
                return result;
            }
        }

        public async Task CreateWallet(string pubPassphrase, string privPassphrase, byte[] seed)
        {
            var client = WalletLoaderService.NewClient(_channel);
            var request = new CreateWalletRequest
            {
                PublicPassphrase = ByteString.CopyFromUtf8(pubPassphrase),
                PrivatePassphrase = ByteString.CopyFromUtf8(privPassphrase),
                Seed = ByteString.CopyFrom(seed),
            };
            await client.CreateWalletAsync(request, cancellationToken: _tokenSource.Token);
        }

        public async Task OpenWallet(string pubPassphrase)
        {
            var client = WalletLoaderService.NewClient(_channel);
            var request = new OpenWalletRequest
            {
                PublicPassphrase = ByteString.CopyFromUtf8(pubPassphrase),
            };
            await client.OpenWalletAsync(request, cancellationToken: _tokenSource.Token);
        }

        public async Task CloseWallet()
        {
            try
            {
                var client = WalletLoaderService.NewClient(_channel);
                await client.CloseWalletAsync(new CloseWalletRequest());
            }
            catch (Exception ex)
            {
                Console.WriteLine(ex);
            }
        }

        public async Task<NetworkResponse> NetworkAsync()
        {
            var client = WalletService.NewClient(_channel);
            return await client.NetworkAsync(new NetworkRequest(), cancellationToken: _tokenSource.Token);
        }

        public async Task<AccountsResponse> AccountsAsync()
        {
            var client = WalletService.NewClient(_channel);
            return await client.AccountsAsync(new AccountsRequest(), cancellationToken: _tokenSource.Token);
        }

        public async Task<TransactionSet> GetTransactionsAsync(int minRecentTransactions, int minRecentBlocks)
        {
            var client = WalletService.NewClient(_channel);
            var request = new GetTransactionsRequest
            {
                // TODO: include these.  With these uncommented, all transactions are loaded.
                //StartingBlockHeight = -minRecentBlocks,
                //MinimumRecentTransactions = minRecentTransactions
            };
            var resp = await client.GetTransactionsAsync(request, cancellationToken: _tokenSource.Token);
            var minedTransactions = resp.MinedTransactions.Select(MarshalBlock);
            var unminedTransactions = resp.UnminedTransactions.Select(MarshalWalletTransaction);
            return new TransactionSet(minedTransactions.ToList(), unminedTransactions.ToDictionary(tx => tx.Hash));
        }

        public async Task<string> NextExternalAddressAsync(Account account)
        {
            var client = WalletService.NewClient(_channel);
            var request = new NextAddressRequest
            {
                Account = account.AccountNumber,
                Kind = NextAddressRequest.Types.Kind.BIP0044_EXTERNAL,
            };
            var resp = await client.NextAddressAsync(request, cancellationToken: _tokenSource.Token);
            return resp.Address;
        }

        public async Task<string> NextInternalAddressAsync(Account account)
        {
            var client = WalletService.NewClient(_channel);
            var request = new NextAddressRequest
            {
                Account = account.AccountNumber,
                Kind = NextAddressRequest.Types.Kind.BIP0044_INTERNAL,
            };
            var resp = await client.NextAddressAsync(request, cancellationToken: _tokenSource.Token);
            return resp.Address;
        }

        public async Task<Account> NextAccountAsync(string passphrase, string accountName)
        {
            if (passphrase == null)
                throw new ArgumentNullException(nameof(passphrase));
            if (accountName == null)
                throw new ArgumentNullException(nameof(accountName));

            var client = WalletService.NewClient(_channel);
            var request = new NextAccountRequest
            {
                Passphrase = ByteString.CopyFromUtf8(passphrase),
                AccountName = accountName,
            };
            var resp = await client.NextAccountAsync(request, cancellationToken: _tokenSource.Token);
            return new Account(resp.AccountNumber);
        }

        public async Task ImportPrivateKeyAsync(Account account, string privateKeyWif, bool rescan, string passphrase)
        {
            if (privateKeyWif == null)
                throw new ArgumentNullException(nameof(privateKeyWif));
            if (passphrase == null)
                throw new ArgumentNullException(nameof(passphrase));

            var client = WalletService.NewClient(_channel);
            var request = new ImportPrivateKeyRequest
            {
                Account = account.AccountNumber,
                PrivateKeyWif = privateKeyWif,
                Rescan = rescan,
                Passphrase = ByteString.CopyFromUtf8(passphrase), // Poorly named: this outputs UTF8 from a UTF16 System.String
            };
            await client.ImportPrivateKeyAsync(request, cancellationToken: _tokenSource.Token);
        }

        public async Task RenameAccountAsync(Account account, string newAccountName)
        {
            if (newAccountName == null)
                throw new ArgumentNullException(nameof(newAccountName));

            var client = WalletService.NewClient(_channel);
            var request = new RenameAccountRequest
            {
                AccountNumber = account.AccountNumber,
                NewName = newAccountName,
            };
            await client.RenameAccountAsync(request, cancellationToken: _tokenSource.Token);
        }

        public async Task PublishTransactionAsync(byte[] signedTransaction)
        {
            if (signedTransaction == null)
                throw new ArgumentNullException(nameof(signedTransaction));

            var client = WalletService.NewClient(_channel);
            var request = new PublishTransactionRequest
            {
                SignedTransaction = ByteString.CopyFrom(signedTransaction),
            };
            await client.PublishTransactionAsync(request, cancellationToken: _tokenSource.Token);
        }

        public async Task<Tuple<List<UnspentOutput>, Amount>> SelectUnspentOutputs(Account account, Amount targetAmount,
            int requiredConfirmations)
        {
            var client = WalletService.NewClient(_channel);
            var request = new FundTransactionRequest
            {
                Account = account.AccountNumber,
                TargetAmount = targetAmount,
                RequiredConfirmations = requiredConfirmations,
                IncludeImmatureCoinbases = false,
                IncludeChangeScript = false,
            };
            var response = await client.FundTransactionAsync(request, cancellationToken: _tokenSource.Token);
            var outputs = response.SelectedOutputs.Select(MarshalUnspentOutput).ToList();
            var total = (Amount)response.TotalAmount;
            return Tuple.Create(outputs, total);
        }

        public async Task<Tuple<List<UnspentOutput>, Amount, OutputScript>> FundTransactionAsync(
            Account account, Amount targetAmount, int requiredConfirmations)
        {
            var client = WalletService.NewClient(_channel);
            var request = new FundTransactionRequest
            {
                Account = account.AccountNumber,
                TargetAmount = targetAmount,
                RequiredConfirmations = requiredConfirmations,
                IncludeImmatureCoinbases = false,
                IncludeChangeScript = true,
            };
            var response = await client.FundTransactionAsync(request, cancellationToken: _tokenSource.Token);
            var outputs = response.SelectedOutputs.Select(MarshalUnspentOutput).ToList();
            var total = (Amount)response.TotalAmount;
            var changeScript = (OutputScript)null;
            if (response.ChangePkScript?.Length != 0)
            {
                changeScript = OutputScript.ParseScript(response.ChangePkScript.ToByteArray());
            }
            return Tuple.Create(outputs, total, changeScript);
        }

        public async Task<Tuple<Transaction, bool>> SignTransactionAsync(string passphrase, Transaction tx)
        {
            var client = WalletService.NewClient(_channel);
            var request = new SignTransactionRequest
            {
                Passphrase = ByteString.CopyFromUtf8(passphrase),
                SerializedTransaction = ByteString.CopyFrom(tx.Serialize()),
            };
            var response = await client.SignTransactionAsync(request, cancellationToken: _tokenSource.Token);
            var signedTransaction = Transaction.Deserialize(response.Transaction.ToByteArray());
            var complete = response.UnsignedInputIndexes.Count == 0;
            return Tuple.Create(signedTransaction, complete);
        }

        /// <summary>
        /// Begins synchronization of the client with the remote wallet process.
        /// A delegate must be passed to be connected to the wallet's ChangesProcessed event to avoid
        /// a race where additional notifications are processed in the sync task before the caller
        /// can connect the event.  The caller is responsible for disconnecting the delegate from the
        /// event handler when finished.
        /// </summary>
        /// <param name="walletEventHandler">Event handler for changes to wallet as new transactions are processed.</param>
        /// <returns>The synced Wallet and the Task that is keeping the wallet in sync.</returns>
        public async Task<Tuple<Wallet, Task>> Synchronize(EventHandler<Wallet.ChangesProcessedEventArgs> walletEventHandler)
        {
            if (walletEventHandler == null)
                throw new ArgumentNullException(nameof(walletEventHandler));

            TransactionNotifications notifications;
            Task notificationsTask;

            // TODO: Initialization requests need timeouts.

            // Loop until synchronization did not race on a reorg.
            while (true)
            {
                // Begin receiving notifications for new and removed wallet transactions before
                // old transactions are downloaded.  Any received notifications are saved to
                // a buffer and are processed after GetAllTransactionsAsync is awaited.
                notifications = new TransactionNotifications(_channel, _tokenSource.Token);
                notificationsTask = notifications.ListenAndBuffer();

                var networkTask = NetworkAsync();
                var accountsTask = AccountsAsync();

                var networkResp = await networkTask;
                var activeBlockChain = BlockChainIdentity.FromNetworkBits(networkResp.ActiveNetwork);

                var txSetTask = GetTransactionsAsync(Wallet.MinRecentTransactions, Wallet.NumRecentBlocks(activeBlockChain));

                var txSet = await txSetTask;
                var rpcAccounts = await accountsTask;

                var lastAccountBlockHeight = rpcAccounts.CurrentBlockHeight;
                var lastAccountBlockHash = new Blake256Hash(rpcAccounts.CurrentBlockHash.ToByteArray());
                var lastTxBlock = txSet.MinedTransactions.LastOrDefault();
                if (lastTxBlock != null)
                {
                    var lastTxBlockHeight = lastTxBlock.Height;
                    var lastTxBlockHash = lastTxBlock.Hash;
                    if (lastTxBlockHeight > lastAccountBlockHeight ||
                        (lastTxBlockHeight == lastAccountBlockHeight && !lastTxBlockHash.Equals(lastAccountBlockHash)))
                    {
                        _tokenSource.Cancel();
                        continue;
                    }
                }

                // Read all received notifications thus far and determine if synchronization raced
                // on a chain reorganize.  Try again if so.
                IList<WalletChanges> transactionNotifications;
                if (notifications.Buffer.TryReceiveAll(out transactionNotifications))
                {
                    if (transactionNotifications.Any(r => r.DetachedBlocks.Count != 0))
                    {
                        _tokenSource.Cancel();
                        continue;
                    }

                    // Skip all attached block notifications that are in blocks lower than the
                    // block accounts notification.  If blocks exist at or past that height,
                    // the first's hash should equal that from the accounts notification.
                    //
                    // This ensures that both notifications contain data that is valid at this
                    // block.
                    var remainingNotifications = transactionNotifications
                        .SelectMany(r => r.AttachedBlocks)
                        .SkipWhile(b => b.Height < lastAccountBlockHeight)
                        .ToList();
                    if (remainingNotifications.Count != 0)
                    {
                        if (!remainingNotifications[0].Hash.Equals(lastAccountBlockHash))
                        {
                            _tokenSource.Cancel();
                            continue;
                        }
                    }

                    // TODO: Merge remaining notifications with the transaction set.
                    // For now, be lazy and start the whole sync over.
                    if (remainingNotifications.Count > 1)
                    {
                        _tokenSource.Cancel();
                        continue;
                    }
                }

                var accounts = rpcAccounts.Accounts.ToDictionary(
                    a => new Account(a.AccountNumber),
                    a => new AccountProperties
                    {
                        AccountName = a.AccountName,
                        TotalBalance = a.TotalBalance,
                        // TODO: uncomment when added to protospec and implemented by wallet.
                        //ImmatureCoinbaseReward = a.ImmatureBalance,
                        ExternalKeyCount = a.ExternalKeyCount,
                        InternalKeyCount = a.InternalKeyCount,
                        ImportedKeyCount = a.ImportedKeyCount,
                    });
                var chainTip = new BlockIdentity(lastAccountBlockHash, lastAccountBlockHeight);
                var wallet = new Wallet(activeBlockChain, txSet, accounts, chainTip);
                wallet.ChangesProcessed += walletEventHandler;

                var syncTask = Task.Run(async () =>
                {
                    var client = WalletService.NewClient(_channel);
                    var accountsStream = client.AccountNotifications(new AccountNotificationsRequest(), cancellationToken: _tokenSource.Token);
                    var accountChangesTask = accountsStream.ResponseStream.MoveNext();
                    var txChangesTask = notifications.Buffer.OutputAvailableAsync();
                    while (true)
                    {
                        var completedTask = await Task.WhenAny(accountChangesTask, txChangesTask);
                        if (!await completedTask)
                        {
                            break;
                        }
                        if (completedTask == accountChangesTask)
                        {
                            var accountProperties = accountsStream.ResponseStream.Current;
                            var account = new Account(accountProperties.AccountNumber);
                            wallet.UpdateAccountProperties(account, accountProperties.AccountName,
                                accountProperties.ExternalKeyCount, accountProperties.InternalKeyCount,
                                accountProperties.ImportedKeyCount);
                            accountChangesTask = accountsStream.ResponseStream.MoveNext();
                        }
                        else if (completedTask == txChangesTask)
                        {
                            var changes = notifications.Buffer.Receive();
                            wallet.ApplyTransactionChanges(changes);
                        }
                    }

                    await notificationsTask;
                });

                return Tuple.Create(wallet, syncTask);
            }
        }
    }
}
