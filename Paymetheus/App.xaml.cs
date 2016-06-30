// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using IniParser;
using IniParser.Model;
using Paymetheus.Decred;
using Paymetheus.Framework;
using Paymetheus.Rpc;
using Paymetheus.ViewModels;
using System;
using System.IO;
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

            InitializeComponent();

            SingletonViewModelLocator.RegisterFactory<ShellView, ShellViewModel>();
            SingletonViewModelLocator.RegisterFactory<Overview, OverviewViewModel>();
            SingletonViewModelLocator.RegisterFactory<Request, RequestViewModel>();
            SingletonViewModelLocator.RegisterFactory<Send, CreateTransactionViewModel>();

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

        public BlockChainIdentity ActiveNetwork { get; private set; }
        internal SynchronizerViewModel Synchronizer { get; private set; }

        private bool _walletLoaded;

        public void MarkWalletLoaded()
        {
            _walletLoaded = true;
        }

        public ConsensusServerRpcOptions DefaultCSRPO { get; private set; }

        private void Application_Startup(object sender, StartupEventArgs e)
        {
            var args = ProcessArguments.ParseArguments(e.Args);

            var activeNetwork = args.IntendedNetwork;

            WalletClient.Initialize();

            var appDataDir = Portability.LocalAppData(Environment.OSVersion.Platform,
                AssemblyResources.Organization, AssemblyResources.ProductName);

            Directory.CreateDirectory(appDataDir);

            // try to obtain some default rpc settings to autofill the startup dialogs with.
            // try paymetheus defaults first, and if that fails, look for a dcrd config.
            try
            {
                var iniParser = new FileIniDataParser();
                IniData config = null;
                string defaultsFile = Path.Combine(appDataDir, "defaults.ini");
                if (File.Exists(defaultsFile))
                {
                    config = iniParser.ReadFile(defaultsFile);
                }
                else
                {
                    var consensusRpcAppData = Portability.LocalAppData(Environment.OSVersion.Platform,
                        "", ConsensusServerRpcOptions.ApplicationName);
                    var consensusRpcConfig = ConsensusServerRpcOptions.ApplicationName + ".conf";
                    var consensusConfigFilePath = Path.Combine(consensusRpcAppData, consensusRpcConfig);
                    if (File.Exists(consensusConfigFilePath))
                    {
                        config = iniParser.ReadFile(consensusConfigFilePath);
                    }
                }

                if (config != null)
                {
                    // Settings can be found in either the Application Options or global sections.
                    var section = config["Application Options"];
                    if (section == null)
                        section = config.Global;

                    var rpcUser = section["rpcuser"] ?? "";
                    var rpcPass = section["rpcpass"] ?? "";
                    var rpcListen = section["rpclisten"] ?? "";
                    var rpcCert = section["rpccert"] ?? "";

                    // rpclisten and rpccert can be filled with sensible defaults when empty.  user and password can not.
                    if (rpcListen == "")
                    {
                        rpcListen = "127.0.0.1";
                    }
                    if (rpcCert == "")
                    {
                        var localCertPath = ConsensusServerRpcOptions.LocalCertificateFilePath();
                        if (File.Exists(localCertPath))
                            rpcCert = localCertPath;
                    }

                    DefaultCSRPO = new ConsensusServerRpcOptions(rpcListen, rpcUser, rpcPass, rpcCert);
                }
            }
            catch { } // Ignore any errors, this will just result in leaving defaults empty.

            var syncTask = Task.Run(async () =>
            {
                return await SynchronizerViewModel.Startup(activeNetwork, appDataDir);
            });
            var synchronizer = syncTask.Result;

            SingletonViewModelLocator.RegisterInstance("Synchronizer", synchronizer);
            ActiveNetwork = activeNetwork;
            Synchronizer = synchronizer;
            Current.Exit += Application_Exit;
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
            var walletClient = Synchronizer.WalletRpcClient;
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

            Synchronizer.WalletRpcProcess.KillIfExecuting();
        }

        private void UncleanShutdown()
        {
            // Ensure that the wallet process is not left running in the background
            // (which can cause issues when starting the application a second time).
            // If the wallet was opened over RPC, attempt to close it first if the
            // client connection is still active.
            try
            {
                var walletClient = Synchronizer.WalletRpcClient;
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
                Synchronizer.WalletRpcProcess?.KillIfExecuting();
            }
        }
    }
}
