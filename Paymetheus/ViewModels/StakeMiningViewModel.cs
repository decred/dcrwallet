using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using Grpc.Core;
using Paymetheus.Decred.Util;
using System;
using System.Windows;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    public class StakeMiningViewModel : ViewModelBase
    {
        public StakeMiningViewModel() : base()
        {
            FetchStakeInfoCommand = new DelegateCommand(FetchStakeInfoAsync);
            FetchStakeInfoCommand.Execute(null);
        }

        private StakeInfoProperties _stakeInfoProperties;
        public StakeInfoProperties StakeInfoProperties
        {
            get { return _stakeInfoProperties; }
            internal set { _stakeInfoProperties = value; RaisePropertyChanged(); }
        }

        private StakeDifficultyProperties _stakeDifficultyProperties;
        public StakeDifficultyProperties StakeDifficultyProperties
        {
            get { return _stakeDifficultyProperties; }
            internal set { _stakeDifficultyProperties = value; RaisePropertyChanged(); }
        }

        public ICommand FetchStakeInfoCommand { get; }

        private async void FetchStakeInfoAsync()
        {
            try
            {
                StakeInfoProperties = await App.Current.Synchronizer.WalletRpcClient.StakeInfoAsync();
                StakeDifficultyProperties = await App.Current.Synchronizer.WalletRpcClient.StakeDifficultyAsync();
            }
            // Do not give an error if a failed precondition response is given. This generally means that 
            // the wallet is synchronizing to the latest block, and if the user waits a short period of 
            // time and retries there will be able to see the appropriate result.
            catch (RpcException ex) when (ex.Status.StatusCode == StatusCode.FailedPrecondition)
            {
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
        }
    }
}
