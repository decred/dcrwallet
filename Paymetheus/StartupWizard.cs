// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using Paymetheus.Bitcoin.Util;
using Paymetheus.Bitcoin.Wallet;
using Paymetheus.Rpc;
using System;
using System.IO;
using System.Windows;

namespace Paymetheus
{
    sealed class StartupWizard : WizardViewModelBase
    {
        public StartupWizard(ShellViewModel shell) : base(shell)
        {
            CurrentDialog = new ConsensusServerRpcConnectionDialog(this);
            Shell = shell;
        }

        public ShellViewModel Shell { get; }

        public event EventHandler WalletOpened;

        public void InvokeWalletOpened()
        {
            App.Current.MarkWalletLoaded();
            WalletOpened?.Invoke(this, new EventArgs());
        }
    }

    class ConnectionWizardDialog : WizardDialogViewModelBase
    {
        public ConnectionWizardDialog(StartupWizard wizard) : base(wizard.Shell, wizard)
        {
            Wizard = wizard;
        }

        public StartupWizard Wizard { get; }
    }

    sealed class ConsensusServerRpcConnectionDialog : ConnectionWizardDialog
    {
        public ConsensusServerRpcConnectionDialog(StartupWizard wizard) : base(wizard)
        {
            ConnectCommand = new DelegateCommand(Connect);

            // Do not autofill local defaults if they don't exist.
            if (!File.Exists(ConsensusServerCertificateFile))
            {
                ConsensusServerNetworkAddress = "";
                ConsensusServerCertificateFile = "";
            }
        }

        public string ConsensusServerApplicationName => ConsensusServerRpcOptions.ApplicationName;
        public string CurrencyName => BlockChain.CurrencyName;
        public string ConsensusServerNetworkAddress { get; set; } = "localhost";
        public string ConsensusServerRpcUsername { get; set; } = "";
        public string ConsensusServerRpcPassword { private get; set; } = "";
        public string ConsensusServerCertificateFile { get; set; } = ConsensusServerRpcOptions.LocalCertificateFilePath();

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
                    await App.Current.WalletRpcClient.StartBtcdRpc(rpcOptions);
                }
                catch (Exception ex) when (ErrorHandling.IsTransient(ex) || ErrorHandling.IsClientError(ex))
                {
                    MessageBox.Show($"Unable to start {ConsensusServerRpcOptions.ApplicationName} RPC.\n\nCheck connection settings and try again.", "Error");
                    MessageBox.Show(ex.Message);
                    return;
                }

                var walletExists = await App.Current.WalletRpcClient.WalletExistsAsync();
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

        // TODO: Convert seed using a wordlist instead of encoding as hex so this is easier
        // for the user to write down and type into the next dialog.
        public string Bip0032SeedHex => Hexadecimal.Encode(_randomSeed);

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
                    Wizard.CurrentDialog = new ConfirmSeedBackupDialog(Wizard, _randomSeed, this);
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
        public ConfirmSeedBackupDialog(StartupWizard wizard, byte[] seed, CreateOrImportSeedDialog previousDialog)
            : base(wizard)
        {
            _seed = seed;
            _previousDialog = previousDialog;

            ConfirmSeedCommand = new DelegateCommand(ConfirmSeed);
            BackCommand = new DelegateCommand(Back);
        }

        private byte[] _seed;
        private CreateOrImportSeedDialog _previousDialog;

        public string Input { get; set; } = "";

        public DelegateCommand ConfirmSeedCommand { get; }
        private void ConfirmSeed()
        {
            byte[] decodedSeed;
            if (Hexadecimal.TryDecode(Input, out decodedSeed))
            {
                if (ValueArray.ShallowEquals(_seed, decodedSeed))
                {
                    _wizard.CurrentDialog = new PromptPassphrasesDialog(Wizard, _seed);
                    return;
                }
            }

            MessageBox.Show("Seed does not match");
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

                await App.Current.WalletRpcClient.CreateWallet(publicPassphrase, PrivatePassphrase, _seed);

                ValueArray.Zero(_seed);
                Wizard.InvokeWalletOpened();
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
                await App.Current.WalletRpcClient.OpenWallet(PublicPassphrase);
                Wizard.InvokeWalletOpened();
            }
            catch (Exception ex) when (ErrorHandling.IsClientError(ex))
            {
                MessageBox.Show("Public data decryption was unsuccessful");
                MessageBox.Show(ex.ToString());
                return;
            }
            finally
            {
                OpenWalletCommand.Executable = true;
            }
        }
    }
}
