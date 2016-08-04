// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Windows.Input;

namespace Paymetheus.Framework
{
    public class DialogViewModelBase : ViewModelBase
    {
        public DialogViewModelBase(ShellViewModelBase shell) : base()
        {
            Shell = shell;
            HideDialogCommand = new DelegateCommand(HideDialog);
        }

        ShellViewModelBase Shell;

        public ICommand HideDialogCommand { get; }

        public void HideDialog()
        {
            Shell.HideDialog();
        }
    }
}
