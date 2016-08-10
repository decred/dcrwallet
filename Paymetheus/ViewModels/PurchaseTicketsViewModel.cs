using Paymetheus.Decred;
using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using Grpc.Core;
using System;
using System.Windows;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    class PurchaseTicketsViewModel : ViewModelBase
    {
        public PurchaseTicketsViewModel() : base()
        {
            var synchronizer = ViewModelLocator.SynchronizerViewModel as SynchronizerViewModel;
            if (synchronizer != null)
            {
                SelectedAccount = synchronizer.Accounts[0];
            }

            FetchStakeDifficultyCommand = new DelegateCommand(FetchStakeDifficultyAsync);
            FetchStakeDifficultyCommand.Execute(null);

            _purchaseTickets = new DelegateCommand(PurchaseTicketsAction);
            _purchaseTickets.Executable = false;
        }

        private DelegateCommand _purchaseTickets;
        public ICommand Execute => _purchaseTickets;

        private bool _poolChecked = false;
        public bool PoolChecked
        {
            get { return _poolChecked; }
            set { _poolChecked = value; RaisePropertyChanged(); }
        }

        private bool _feesChecked = false;
        public bool FeesChecked
        {
            get { return _feesChecked; }
            set { _feesChecked = value; RaisePropertyChanged(); }
        }

        private AccountViewModel _selectedAccount;
        public AccountViewModel SelectedAccount
        {
            get { return _selectedAccount; }
            set { _selectedAccount = value; RaisePropertyChanged(); }
        }

        private Address _ticketAddress = null;
        public string TicketAddressString
        {
            get { return _ticketAddress?.ToString() ?? ""; }
            set
            {
                _ticketAddress = null;
                if (value == "")
                    return;

                try
                {
                    _ticketAddress = Address.Decode(value);
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        private Address _poolAddress = null;
        public string PoolAddressString
        {
            get { return _poolAddress?.ToString() ?? ""; }
            set
            {
                _poolAddress = null;
                if (value == "")
                    return;

                try
                {
                    _poolAddress = Address.Decode(value);
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        private decimal _poolFees = 0.0M;
        public decimal PoolFees
        {
            get { return _poolFees; }
            set
            {
                try
                {
                    var testPoolFees = value * 100.0M;
                    if (testPoolFees != Math.Floor(testPoolFees))
                        throw new ArgumentException("pool fees must have two decimal points of precision maximum");
                    if (value > 100.0M)
                        throw new ArgumentException("pool fees must be less or equal too than 100.00%");
                    if (value < 0.01M)
                        throw new ArgumentException("pool fees must be greater than or equal to 0.01%");
                    _poolFees = value;
                }
                catch
                {
                    _poolFees = 0.0M;
                    throw;
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        private uint _ticketsToPurchase = 0;
        public uint TicketsToPurchase
        {
            get { return _ticketsToPurchase; }
            set
            {
                try
                {
                    _ticketsToPurchase = value;
                }
                catch
                {
                    _ticketsToPurchase = 0;
                    throw;
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        // The default expiry is 16.
        private uint _expiry = 16;
        private uint minExpiry = 2;
        public uint Expiry
        {
            get { return _expiry; }
            set
            {
                try
                {
                    if (value < minExpiry)
                        throw new ArgumentException("Expiry must be a minimum of 2 blocks");

                    _expiry = value;
                }
                catch
                {
                    _expiry = 0;
                    throw;
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        // TODO Declare this as a global somewhere?
        private const long minFeePerKb = (long)1e6;
        private const long maxFeePerKb = (long)1e8 - 1;

        private Amount _splitFee = 0;
        public Amount SplitFeeAmount => _splitFee;
        public string SplitFeeString
        {
            get { return _splitFee.ToString(); }
            set
            {
                try
                {
                    var testAmount = Denomination.Decred.AmountFromString(value);

                    if (testAmount < minFeePerKb)
                        throw new ArgumentException($"Too small fee passed (must be >= {(Amount)minFeePerKb} DCR/kB)");

                    if (testAmount > maxFeePerKb)
                        throw new ArgumentException($"Too big fee passed (must be <= {(Amount)minFeePerKb} DCR/kB)");

                    _splitFee = testAmount;
                }
                catch
                {
                    _splitFee = 0;
                    throw;
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        private Amount _ticketFee = 0;
        public Amount TicketFeeAmount => _ticketFee;
        public string TicketFeeString
        {
            get { return _ticketFee.ToString(); }
            set
            {
                try
                {
                    var testAmount = Denomination.Decred.AmountFromString(value);

                    if (testAmount < minFeePerKb)
                        throw new ArgumentException($"Too small fee passed (must be >= {(Amount)minFeePerKb} DCR/kB)");

                    if (testAmount > maxFeePerKb)
                        throw new ArgumentException($"Too big fee passed (must be <= {(Amount)minFeePerKb} DCR/kB)");

                    _ticketFee = testAmount;
                }
                catch
                {
                    _ticketFee = 0;
                    throw;
                }
                finally
                {
                    enableOrDisableSendCommand();
                }
            }
        }

        private void enableOrDisableSendCommand()
        {
            if (_selectedAccount == null)
            {
                _purchaseTickets.Executable = false;
                return;
            }

            if (_ticketAddress == null)
            {
                _purchaseTickets.Executable = false;
                return;
            }

            if (_expiry < minExpiry) {
                _purchaseTickets.Executable = false;
                return;
            }

            if (_ticketsToPurchase <= 0)
            {
                _purchaseTickets.Executable = false;
                return;
            }

            if (_poolChecked)
            {
                if (_poolAddress == null)
                {
                    _purchaseTickets.Executable = false;
                    return;
                }

                if (_poolFees == 0.0M)
                {
                    _purchaseTickets.Executable = false;
                    return;
                }
            }

            if (_feesChecked)
            {
                if (_splitFee == 0)
                {
                    _purchaseTickets.Executable = false;
                    return;
                }

                if (_ticketFee == 0)
                {
                    _purchaseTickets.Executable = false;
                    return;
                }
            }

            // Not enough funds.
            if ((_stakeDifficultyProperties.NextTicketPrice * (Amount)_ticketsToPurchase) > _selectedAccount.Balances.SpendableBalance)
            {
                // TODO: Better inform the user somehow of why it doesn't allow ticket 
                // purchase?
                //
                // string errorString = "Not enough funds; have " +
                //     _selectedAccount.Balances.SpendableBalance.ToString() + " want " +
                //     ((Amount)(_stakeDifficultyProperties.NextTicketPrice * (Amount)_ticketsToPurchase)).ToString();
                // MessageBox.Show(errorString);
                _purchaseTickets.Executable = false;
                return;
            }

            _purchaseTickets.Executable = true;
        }

        private StakeDifficultyProperties _stakeDifficultyProperties;
        public StakeDifficultyProperties StakeDifficultyProperties
        {
            get { return _stakeDifficultyProperties; }
            internal set { _stakeDifficultyProperties = value; RaisePropertyChanged(); }
        }

        public ICommand FetchStakeDifficultyCommand { get; }

        private async void FetchStakeDifficultyAsync()
        {
            try
            {
                StakeDifficultyProperties = await App.Current.Synchronizer.WalletRpcClient.StakeDifficultyAsync();
                int windowSize = 144;
                int heightOfChange = ((StakeDifficultyProperties.HeightForTicketPrice / windowSize) + 1) * windowSize;
                BlocksToRetarget = heightOfChange - StakeDifficultyProperties.HeightForTicketPrice;
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
        }

        private int _blocksToRetarget;
        public int BlocksToRetarget
        {
            get { return _blocksToRetarget; }
            internal set { _blocksToRetarget = value; RaisePropertyChanged(); }
        }

        private void PurchaseTicketsAction()
        {
            var shell = ViewModelLocator.ShellViewModel as ShellViewModel;
            if (shell != null)
            {
                Func<string, Task<bool>> action =
                    passphrase => PurchaseTicketsWithPassphrase(passphrase);
                shell.VisibleDialogContent = new PassphraseDialogViewModel(shell, 
                    "Enter passphrase to purchase tickets", 
                    "PURCHASE", 
                    action);
            }
        }

        private string _responseString = "";
        public string ResponseString
        {
            get { return _responseString; }
            set { _responseString = value; RaisePropertyChanged(); }
        }

        private async Task<bool> PurchaseTicketsWithPassphrase(string passphrase)
        {
            var account = SelectedAccount.Account;
            var spendLimit = StakeDifficultyProperties.NextTicketPrice;
            int requiredConfirms = 2; // TODO allow user to set
            uint expiryHeight = _expiry + (uint)StakeDifficultyProperties.HeightForTicketPrice;

            Amount splitFeeLocal = 0;
            Amount ticketFeeLocal = 0;
            if (_feesChecked)
            {
                splitFeeLocal = _splitFee;
               ticketFeeLocal = _ticketFee;
            }

            List<Blake256Hash> purchaseResponse;
            var walletClient = App.Current.Synchronizer.WalletRpcClient;
            try
            {
                purchaseResponse = await walletClient.PurchaseTicketsAsync(account, spendLimit,
                    requiredConfirms, _ticketAddress, _ticketsToPurchase, _poolAddress, 
                    (double)_poolFees, expiryHeight, _splitFee, _ticketFee, passphrase);
            }
            catch (Exception ex)
            {
                ResponseString = "Unexpected error: " + ex.ToString();
                return false;
            }

            ResponseString = "Success! Ticket hashes:\n" + string.Join("\n", purchaseResponse);
            return true;
        }
    }
}
