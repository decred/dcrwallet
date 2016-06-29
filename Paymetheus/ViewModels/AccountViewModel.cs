// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    public class AccountViewModel : ViewModelBase
    {
        public AccountViewModel(Account account, AccountProperties properties, Balances balances) : base()
        {
            Account = account;
            _accountProperties = properties;
            _balances = balances;

            OpenRenameAccountDialogCommand = new DelegateCommand(OpenRenameAccountDialogAction);
            OpenImportKeyDialogCommand = new DelegateCommand(OpenImportKeyDialogAction);
            HideAccountCommand = new DelegateCommand(HideAccountAction);
        }

        public Account Account { get; }

        public uint AccountNumber => Account.AccountNumber;

        private AccountProperties _accountProperties;
        public AccountProperties AccountProperties
        {
            get { return _accountProperties; }
            internal set { _accountProperties = value; RaisePropertyChanged(); }
        }

        private Balances _balances;
        public Balances Balances
        {
            get { return _balances; }
            internal set { _balances = value; RaisePropertyChanged(); }
        }

        public ICommand OpenRenameAccountDialogCommand { get; }

        private void OpenRenameAccountDialogAction()
        {
            var shell = (ShellViewModel)ViewModelLocator.ShellViewModel;
            shell.ShowDialog(new RenameAccountDialogViewModel(shell, Account, AccountProperties.AccountName));
        }

        public ICommand OpenImportKeyDialogCommand { get; }

        private void OpenImportKeyDialogAction()
        {
            var shell = (ShellViewModel)ViewModelLocator.ShellViewModel;
            shell.ShowDialog(new ImportDialogViewModel(shell, Account));
        }

        public ICommand HideAccountCommand { get; }

        private void HideAccountAction()
        {
            throw new NotImplementedException();
        }
    }
}
