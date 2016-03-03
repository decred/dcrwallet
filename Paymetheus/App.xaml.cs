// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Rpc;
using System;
using System.Diagnostics;
using System.Threading.Tasks;
using System.Windows;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for App.xaml
    /// </summary>
    public partial class App : Application
    {
        public static new App Current { get; private set; }

        public App()
        {
            if (Current != null)
                throw new ApplicationException("Application instance already exists");

            Application.Current.Dispatcher.UnhandledException += (sender, args) =>
            {
                var ex = args.Exception;

                var ae = ex as AggregateException;
                Exception inner;
                if (ae != null && ae.TryUnwrap(out inner))
                {
                    ex = inner;
                }

                MessageBox.Show(ex.Message, "Error");
                UncleanShutdown();
                Application.Current.Shutdown(1);
            };

            Application.Current.Startup += Application_Startup;

            Current = this;
        }

        private bool _walletLoaded;

        public Process WalletRpcProcess { get; private set; }
        public WalletClient WalletRpcClient { get; private set; }

        public void MarkWalletLoaded()
        {
            _walletLoaded = true;
        }

        private void Application_Startup(object sender, StartupEventArgs e)
        {
            WalletClient.Initialize();

            var startupTask = Task.Run(async () =>
            {
                // TODO: Make network selectable (parse e.Args for a network)
                var activeNetwork = BlockChainIdentity.TestNet;

                // Begin the asynchronous reading of the certificate before starting the wallet
                // process.  This uses filesystem events to know when to begin reading the certificate,
                // and if there is too much delay between wallet writing the cert and this process
                // beginning to observe the change, the event may never fire and the cert won't be read.
                var rootCertificateTask = TransportSecurity.ReadModifiedCertificateAsync();

                var walletProcess = WalletProcess.Start(activeNetwork);

                WalletClient walletClient;
                try
                {
                    var listenAddress = WalletProcess.RpcListenAddress("localhost", activeNetwork);
                    var rootCertificate = await rootCertificateTask;
                    walletClient = await WalletClient.ConnectAsync(listenAddress, rootCertificate);
                }
                catch (Exception)
                {
                    if (walletProcess.HasExited)
                    {
                        throw new Exception("Wallet process closed unexpectedly");
                    }
                    walletProcess.KillIfExecuting();
                    throw;
                }

                return Tuple.Create(walletProcess, walletClient);
            });

            startupTask.Wait();
            var startupResult = startupTask.Result;
            WalletRpcProcess = startupResult.Item1;
            WalletRpcClient = startupResult.Item2;
            Application.Current.Exit += Application_Exit;
        }

        private void Application_Exit(object sender, ExitEventArgs e)
        {
            CleanShutdown();
        }

        private void CleanShutdown()
        {
            // Cancel all outstanding requests and notification streams,
            // close the wallet if it was loaded, disconnect the client
            // from the process, and stop the process.
            var walletClient = WalletRpcClient;
            walletClient.CancelRequests();
            Task.Run(async () =>
            {
                if (_walletLoaded)
                {
                    await walletClient.CloseWallet();
                }
                await walletClient.Disconnect();
            }).Wait();
            walletClient.Dispose();

            WalletRpcProcess.KillIfExecuting();
        }

        private void UncleanShutdown()
        {
            // Ensure that the wallet process is not left running in the background
            // (which can cause issues when starting the application a second time).
            // If the wallet was opened over RPC, attempt to close it first if the
            // client connection is still active.
            try
            {
                var walletClient = WalletRpcClient;
                if (walletClient == null)
                    return;

                if (_walletLoaded)
                {
                    Task.Run(walletClient.CloseWallet).Wait();
                }
            }
            catch (Exception) { }
            finally
            {
                WalletRpcProcess?.KillIfExecuting();
            }
        }
    }
}
