// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Grpc.Core;
using IniParser;
using IniParser.Model;
using IniParser.Parser;
using Paymetheus.Decred;
using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using Paymetheus.Rpc;
using System;
using System.IO;
using System.Windows;

namespace Paymetheus.ViewModels
{
    public sealed class StartupWizard : WizardViewModelBase
    {
        public StartupWizard(ShellViewModelBase shell, ConsensusServerRpcOptions csro = null) : base()
        {
            CurrentDialog = new ConsensusServerRpcConnectionDialog(this, csro);
        }

        public void OnFinished()
        {
            App.Current.MarkWalletLoaded();
            Messenger.MessageSingleton<SynchronizerViewModel>(new StartupWizardFinishedMessage());
        }
    }

    class ConnectionWizardDialog : WizardDialogViewModelBase
    {
        public ConnectionWizardDialog(StartupWizard wizard) : base(wizard)
        {
            Wizard = wizard;
        }

        public StartupWizard Wizard { get; }
    }

    sealed class ConsensusServerRpcConnectionDialog : ConnectionWizardDialog
    {
        public ConsensusServerRpcConnectionDialog(StartupWizard wizard, ConsensusServerRpcOptions csro = null) : base(wizard)
        {
            ConnectCommand = new DelegateCommand(Connect);

            // Apply any discovered RPC defaults.
            if (csro != null)
            {
                ConsensusServerNetworkAddress = csro.NetworkAddress;
                ConsensusServerRpcUsername = csro.RpcUser;
                ConsensusServerRpcPassword = csro.RpcPassword;
                ConsensusServerCertificateFile = csro.CertificatePath;
            }
        }

        public string ConsensusServerApplicationName => ConsensusServerRpcOptions.ApplicationName;
        public string CurrencyName => BlockChain.CurrencyName;
        public string ConsensusServerNetworkAddress { get; set; } = "";
        public string ConsensusServerRpcUsername { get; set; } = "";
        public string ConsensusServerRpcPassword { internal get; set; } = "";
        public string ConsensusServerCertificateFile { get; set; } = "";

        public DelegateCommand ConnectCommand { get; }
        private async void Connect()
        {
            try
            {
                ConnectCommand.Executable = false;

                if (string.IsNullOrWhiteSpace(ConsensusServerNetworkAddress))
                {
                    MessageBox.Show("Network address is required");
                    return;
                }
                if (string.IsNullOrWhiteSpace(ConsensusServerRpcUsername))
                {
                    MessageBox.Show("RPC username is required");
                    return;
                }
                if (ConsensusServerRpcPassword.Length == 0)
                {
                    MessageBox.Show("RPC password may not be empty");
                    return;
                }
                if (!File.Exists(ConsensusServerCertificateFile))
                {
                    MessageBox.Show("Certificate file not found");
                    return;
                }

                var rpcOptions = new ConsensusServerRpcOptions(ConsensusServerNetworkAddress,
                    ConsensusServerRpcUsername, ConsensusServerRpcPassword, ConsensusServerCertificateFile);
                try
                {
                    await App.Current.Synchronizer.WalletRpcClient.StartConsensusRpc(rpcOptions);
                }
                catch (Exception ex) when (ErrorHandling.IsTransient(ex) || ErrorHandling.IsClientError(ex))
                {
                    MessageBox.Show($"Unable to start {ConsensusServerRpcOptions.ApplicationName} RPC.\n\nCheck connection settings and try again.", "Error");
                    MessageBox.Show(ex.Message);
                    return;
                }

                // save defaults to a file so that the user doesn't have to type this information again
                var ini = new IniData();
                ini.Sections.AddSection("Application Options");
                ini["Application Options"]["rpcuser"] = ConsensusServerRpcUsername;
                ini["Application Options"]["rpcpass"] = ConsensusServerRpcPassword;
                ini["Application Options"]["rpclisten"] = ConsensusServerNetworkAddress;
                var appDataDir = Portability.LocalAppData(Environment.OSVersion.Platform,
                    AssemblyResources.Organization, AssemblyResources.ProductName);
                var parser = new FileIniDataParser();
                parser.WriteFile(Path.Combine(appDataDir, "defaults.ini"), ini);

                var walletExists = await App.Current.Synchronizer.WalletRpcClient.WalletExistsAsync();
                if (!walletExists)
                {
                    _wizard.CurrentDialog = new CreateOrImportSeedDialog(Wizard);
                }
                else
                {
                    // TODO: Determine whether the public encryption is enabled and a prompt for the
                    // public passphrase prompt is needed before the wallet can be opened.  If it
                    // does not, then the wallet can be opened directly here instead of creating
                    // another dialog.
                    _wizard.CurrentDialog = new PromptPublicPassphraseDialog(Wizard);

                    //await _walletClient.OpenWallet("public");
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
            finally
            {
                ConnectCommand.Executable = true;
            }
        }
    }

    sealed class CreateOrImportSeedDialog : ConnectionWizardDialog
    {
        public CreateOrImportSeedDialog(StartupWizard wizard) : base(wizard)
        {
            _randomSeed = WalletSeed.GenerateRandomSeed();

            ContinueCommand = new DelegateCommand(Continue);
            ContinueCommand.Executable = false;
        }

        private byte[] _randomSeed;
        private PgpWordList _pgpWordList = new PgpWordList();

        public string Bip0032SeedHex => Hexadecimal.Encode(_randomSeed);
        public string Bip0032SeedWordList => string.Join(" ", WalletSeed.EncodeWordList(_pgpWordList, _randomSeed));

        private bool _createChecked;
        public bool CreateChecked
        {
            get { return _createChecked; }
            set
            {
                _createChecked = value;
                RaisePropertyChanged();
                ContinueCommand.Executable = true;
            }
        }

        private bool _importChecked;
        public bool ImportChecked
        {
            get { return _importChecked; }
            set
            {
                _importChecked = value;
                RaisePropertyChanged();
                ContinueCommand.Executable = true;
            }
        }

        public string ImportedSeed { get; set; }

        public DelegateCommand ContinueCommand { get; }
        private void Continue()
        {
            try
            {
                ContinueCommand.Executable = false;

                if (CreateChecked)
                {
                    Wizard.CurrentDialog = new ConfirmSeedBackupDialog(Wizard, this, _randomSeed, _pgpWordList);
                }
                else
                {
                    var seed = WalletSeed.DecodeAndValidateUserInput(ImportedSeed, _pgpWordList);
                    Wizard.CurrentDialog = new PromptPassphrasesDialog(Wizard, seed);
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
            finally
            {
                ContinueCommand.Executable = true;
            }
        }
    }

    sealed class ConfirmSeedBackupDialog : ConnectionWizardDialog
    {
        public ConfirmSeedBackupDialog(StartupWizard wizard, CreateOrImportSeedDialog previousDialog,
            byte[] seed, PgpWordList pgpWordlist)
            : base(wizard)
        {
            _previousDialog = previousDialog;
            _seed = seed;
            _pgpWordList = pgpWordlist;

            ConfirmSeedCommand = new DelegateCommand(ConfirmSeed);
            BackCommand = new DelegateCommand(Back);
        }

        private CreateOrImportSeedDialog _previousDialog;
        private byte[] _seed;
        private PgpWordList _pgpWordList;

        public string Input { get; set; } = "";

        public DelegateCommand ConfirmSeedCommand { get; }
        private void ConfirmSeed()
        {
            try
            {
                ConfirmSeedCommand.Executable = false;

                // When on testnet, allow clicking through the dialog without any validation.
                if (App.Current.ActiveNetwork == BlockChainIdentity.TestNet)
                {
                    if (Input.Length == 0)
                    {
                        _wizard.CurrentDialog = new PromptPassphrasesDialog(Wizard, _seed);
                        return;
                    }
                }

                var decodedSeed = WalletSeed.DecodeAndValidateUserInput(Input, _pgpWordList);
                if (ValueArray.ShallowEquals(_seed, decodedSeed))
                {
                    _wizard.CurrentDialog = new PromptPassphrasesDialog(Wizard, _seed);
                }
                else
                {
                    MessageBox.Show("Seed does not match");
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Invalid seed");
            }
            finally
            {
                ConfirmSeedCommand.Executable = true;
            }
        }

        public DelegateCommand BackCommand { get; }
        private void Back()
        {
            Wizard.CurrentDialog = _previousDialog;
        }
    }

    sealed class PromptPassphrasesDialog : ConnectionWizardDialog
    {
        public PromptPassphrasesDialog(StartupWizard wizard, byte[] seed) : base(wizard)
        {
            _seed = seed;

            CreateWalletCommand = new DelegateCommand(CreateWallet);
        }

        private byte[] _seed;

        private bool _usePublicEncryption;
        public bool UsePublicEncryption
        {
            get { return _usePublicEncryption; }
            set { _usePublicEncryption = value; RaisePropertyChanged(); }
        }
        public string PublicPassphrase { private get; set; } = "";
        public string PrivatePassphrase { private get; set; } = "";

        public DelegateCommand CreateWalletCommand { get; }
        private async void CreateWallet()
        {
            try
            {
                CreateWalletCommand.Executable = false;

                if (string.IsNullOrEmpty(PrivatePassphrase))
                {
                    MessageBox.Show("Private passphrase is required");
                    return;
                }

                var publicPassphrase = PublicPassphrase;
                if (!UsePublicEncryption)
                {
                    publicPassphrase = "public";
                }
                else
                {
                    if (string.IsNullOrEmpty(publicPassphrase))
                    {
                        MessageBox.Show("Public passphrase is required");
                        return;
                    }
                }

                await App.Current.Synchronizer.WalletRpcClient.CreateWallet(publicPassphrase, PrivatePassphrase, _seed);

                ValueArray.Zero(_seed);
                Wizard.OnFinished();
            }
            finally
            {
                CreateWalletCommand.Executable = true;
            }
        }
    }

    sealed class PromptPublicPassphraseDialog : ConnectionWizardDialog
    {
        public PromptPublicPassphraseDialog(StartupWizard wizard) : base(wizard)
        {
            OpenWalletCommand = new DelegateCommand(OpenWallet);
        }

        public string PublicPassphrase { get; set; } = "";

        public DelegateCommand OpenWalletCommand { get; }
        private async void OpenWallet()
        {
            try
            {
                OpenWalletCommand.Executable = false;
                await App.Current.Synchronizer.WalletRpcClient.OpenWallet(PublicPassphrase);
                Wizard.OnFinished();
            }
            catch (RpcException ex) when (ex.Status.StatusCode == StatusCode.InvalidArgument)
            {
                MessageBox.Show("Incorrect passphrase");
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.ToString());
            }
            finally
            {
                OpenWalletCommand.Executable = true;
            }
        }
    }
}
